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
# Recorded, by set (SETS, default "validators accounts"):
#   validators:
#     staking/Validators.json, slashing/SigningInfos.json, slashing/Params.json
#     per validator: staking/Delegation.<owner>.<operator>.json (self-delegation)
#       and staking/ValidatorDelegations.<operator>.json (count_total)
#     per operator in VALIDATORS: staking/Validator.<operator>.json and
#       slashing/SigningInfo.<consaddr>.json (the single-validator resync)
#   accounts, per address in ACCOUNTS (the account page's live reads):
#     bank/AllBalances.<address>.json, staking/DelegatorDelegations.<address>.json,
#     staking/DelegatorUnbondingDelegations.<address>.json,
#     distribution/DelegationTotalRewards.<address>.json
#   denoms (the denoms sync):
#     bank/TotalSupply.json, bank/DenomsMetadata.json, tradebin/AllMarkets.json
#     per factory denom: tokenfactory/DenomAuthority.<denom>.json
#     per denom in DENOMS (the single-denom resync):
#       bank/DenomMetadataByQueryString.<denom>.json (a 404 for a denom without
#       metadata) and bank/SupplyOf.<denom>.json
#     tokenfactory AllDenomBranding and tradebin HaltedDenoms are recorded only
#     from a gateway that serves them (chain v8.2.0 and later): an older node
#     answers 501, and with no file the fake answers Unimplemented the same way.
#   Denoms in file names are path-escaped (factory%2Fbze1…%2Fuvdl).
#   gov (the proposals sync):
#     gov/Proposals.json (every proposal), gov/Proposals.PROPOSAL_STATUS_VOTING_PERIOD.json
#     (the minute refresh's list), staking/Pool.json (bonded tokens for turnout)
#     per id in PROPOSALS: gov/Proposal.<id>.json and gov/TallyResult.<id>.json
#
# Usage: VALIDATORS="bzevaloper1…" ACCOUNTS="bze1…" scripts/record-grpc-fixtures.sh
#        (or: make grpc-fixtures [SETS=accounts] VALIDATORS="…" ACCOUNTS="…" [DENOMS="ubze …"]
#         [PROPOSALS="47 …"] [REST=…])
set -euo pipefail

REST="${REST:-https://rest.getbze.com}"
REST="${REST%/}"
SETS="${SETS:-validators accounts}"
VALIDATORS="${VALIDATORS:-}"
ACCOUNTS="${ACCOUNTS:-}"
DENOMS="${DENOMS:-}"
PROPOSALS="${PROPOSALS:-}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${ROOT}/internal/testutil/fakenode/testdata/grpc"
mkdir -p "${OUT}/staking" "${OUT}/slashing" "${OUT}/bank" "${OUT}/distribution" "${OUT}/tokenfactory" "${OUT}/tradebin" "${OUT}/gov"

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

# esc <denom>: the denom path-escaped, as file names and query strings take it.
esc() { jq -rn --arg d "$1" '$d|@uri'; }

# optional <route-with-query> <destination>: fetch when the gateway serves the
# route, skip on 501 (a query the node's chain version does not have).
optional() {
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' "${REST}/$1")"
  if [[ "${code}" == "501" ]]; then
    echo "skipped $1: HTTP 501, not served by this chain version"
    return
  fi
  fetch "$1" "$2"
}

# has <set>: whether SETS names the set.
has() { [[ " ${SETS} " == *" $1 "* ]]; }

if has validators; then
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
fi

if has accounts; then
  for acc in ${ACCOUNTS}; do
    fetch "cosmos/bank/v1beta1/balances/${acc}?pagination.limit=1000" "bank/AllBalances.${acc}.json"
    fetch "cosmos/staking/v1beta1/delegations/${acc}?pagination.limit=1000" "staking/DelegatorDelegations.${acc}.json"
    fetch "cosmos/staking/v1beta1/delegators/${acc}/unbonding_delegations?pagination.limit=1000" \
      "staking/DelegatorUnbondingDelegations.${acc}.json"
    fetch "cosmos/distribution/v1beta1/delegators/${acc}/rewards" "distribution/DelegationTotalRewards.${acc}.json"
  done
fi

if has denoms; then
  fetch "cosmos/bank/v1beta1/supply?pagination.limit=1000" bank/TotalSupply.json
  fetch "cosmos/bank/v1beta1/denoms_metadata?pagination.limit=1000" bank/DenomsMetadata.json
  fetch "bze/tradebin/all_markets?pagination.limit=1000" tradebin/AllMarkets.json
  optional "bze/tokenfactory/all_denom_branding?pagination.limit=1000" tokenfactory/AllDenomBranding.json
  optional "bze/tradebin/halted_denoms?pagination.limit=1000" tradebin/HaltedDenoms.json

  for d in $( (jq -r '.supply[].denom' "${OUT}/bank/TotalSupply.json"; jq -r '.metadatas[].base' "${OUT}/bank/DenomsMetadata.json") \
      | grep '^factory/' | sort -u); do
    fetch "bze/tokenfactory/denom_authority?denom=$(esc "${d}")" "tokenfactory/DenomAuthority.$(esc "${d}").json"
  done

  for d in ${DENOMS}; do
    fetch "cosmos/bank/v1beta1/denoms_metadata_by_query_string?denom=$(esc "${d}")" \
      "bank/DenomMetadataByQueryString.$(esc "${d}").json" 404
    fetch "cosmos/bank/v1beta1/supply/by_denom?denom=$(esc "${d}")" "bank/SupplyOf.$(esc "${d}").json"
  done
fi

if has gov; then
  fetch "cosmos/gov/v1/proposals?pagination.limit=1000" gov/Proposals.json
  fetch "cosmos/gov/v1/proposals?proposal_status=PROPOSAL_STATUS_VOTING_PERIOD&pagination.limit=1000" \
    gov/Proposals.PROPOSAL_STATUS_VOTING_PERIOD.json
  fetch "cosmos/staking/v1beta1/pool" staking/Pool.json
  for id in ${PROPOSALS}; do
    fetch "cosmos/gov/v1/proposals/${id}" "gov/Proposal.${id}.json"
    fetch "cosmos/gov/v1/proposals/${id}/tally" "gov/TallyResult.${id}.json"
  done
fi
