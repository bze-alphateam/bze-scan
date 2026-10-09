#!/usr/bin/env bash
# Records the fake gRPC server's fixtures (internal/testutil/fakenode/
# testdata/grpc) from a REST gateway: the gateway's JSON is the SDK's proto
# JSON, which the fake unmarshals with the chain codec and answers over gRPC.
#
# Fixtures are committed and re-recorded only on purpose, never as a routine
# refresh. Every later story that queries a new method adds its calls here.
#
# Files are <service>/<Method>[.<key>].json, where the key is the request's
# non-empty string fields in field order joined by "." (see fakenode.GRPC).
# A gateway error (404 for a missing delegation, say) is stored as the error
# body and answered as that gRPC status.
#
# Recorded:
#   staking/Validators.json, slashing/SigningInfos.json, slashing/Params.json
#   per validator: staking/Delegation.<owner>.<operator>.json (self-delegation)
#     and staking/ValidatorDelegations.<operator>.json (count_total)
#   per operator in VALIDATORS: staking/Validator.<operator>.json and
#     slashing/SigningInfo.<consaddr>.json (the single-validator resync)
#
# Usage: VALIDATORS="bzevaloper1…" scripts/record-grpc-fixtures.sh
#        (or: make grpc-fixtures VALIDATORS="…" [REST=…])
set -euo pipefail

REST="${REST:-https://rest.getbze.com}"
REST="${REST%/}"
VALIDATORS="${VALIDATORS:-}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${ROOT}/internal/testutil/fakenode/testdata/grpc"
mkdir -p "${OUT}/staking" "${OUT}/slashing"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT
CONV="${TMPDIR}/bech32conv"
(cd "${ROOT}" && go build -o "${CONV}" ./scripts/bech32conv)

# fetch <route-with-query> <destination> [allow-error]
fetch() {
  local route="$1" dest="${OUT}/$2" allow="${3:-}" tmp code
  tmp="$(mktemp)"
  code="$(curl -sS -o "${tmp}" -w '%{http_code}' "${REST}/${route}")"
  if [[ "${code}" != "200" && ( -z "${allow}" || "${code}" != "${allow}" ) ]]; then
    echo "GET ${route}: HTTP ${code}" >&2
    cat "${tmp}" >&2
    rm -f "${tmp}"
    exit 1
  fi
  jq . "${tmp}" > "${dest}"
  rm -f "${tmp}"
  chmod 644 "${dest}"
  echo "recorded ${route} -> ${dest#"${OUT}/"}"
}

fetch "cosmos/staking/v1beta1/validators?pagination.limit=200" staking/Validators.json
fetch "cosmos/slashing/v1beta1/signing_infos?pagination.limit=200" slashing/SigningInfos.json
fetch "cosmos/slashing/v1beta1/params" slashing/Params.json

for op in $(jq -r '.validators[].operator_address' "${OUT}/staking/Validators.json"); do
  owner="$("${CONV}" bze "${op}")"
  fetch "cosmos/staking/v1beta1/validators/${op}/delegations/${owner}" "staking/Delegation.${owner}.${op}.json" 404
  fetch "cosmos/staking/v1beta1/validators/${op}/delegations?pagination.limit=1&pagination.count_total=true" \
    "staking/ValidatorDelegations.${op}.json"
done

for op in ${VALIDATORS}; do
  fetch "cosmos/staking/v1beta1/validators/${op}" "staking/Validator.${op}.json"
  key="$(jq -r '.validator.consensus_pubkey.key' "${OUT}/staking/Validator.${op}.json")"
  cons="$("${CONV}" consaddr "${key}")"
  fetch "cosmos/slashing/v1beta1/signing_infos/${cons}" "slashing/SigningInfo.${cons}.json"
done
