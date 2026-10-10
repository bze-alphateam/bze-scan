#!/usr/bin/env bash
# Records the fake chain registry's fixtures (internal/testutil/fakeregistry/
# testdata): chain.json and assetlist.json of each chain directory in CHAINS,
# from the cosmos/chain-registry repository as raw.githubusercontent.com
# serves it. A chain without an asset list gets none. The directory listings
# the GitHub contents API answers are not recorded: the fake builds them from
# the directories it holds.
#
# Fixtures are committed and re-recorded only on purpose, never as a routine
# refresh.
#
# Usage: CHAINS="beezee cosmoshub" scripts/record-registry-fixtures.sh
#        (or: make registry-fixtures [CHAINS="beezee cosmoshub noble mirage"] [RAW=…])
set -euo pipefail

RAW="${RAW:-https://raw.githubusercontent.com/cosmos/chain-registry/master}"
RAW="${RAW%/}"
CHAINS="${CHAINS:-beezee cosmoshub noble mirage}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${ROOT}/internal/testutil/fakeregistry/testdata"

for c in ${CHAINS}; do
  mkdir -p "${OUT}/${c}"
  for f in chain.json assetlist.json; do
    tmp="$(mktemp)"
    code="$(curl -sS -o "${tmp}" -w '%{http_code}' "${RAW}/${c}/${f}")"
    case "${code}" in
      200)
        jq . "${tmp}" > "${OUT}/${c}/${f}"
        chmod 644 "${OUT}/${c}/${f}"
        echo "recorded ${c}/${f}"
        ;;
      404)
        [[ "${f}" == assetlist.json ]] || { echo "GET ${c}/${f}: HTTP 404" >&2; exit 1; }
        rm -f "${OUT}/${c}/${f}"
        echo "no ${c}/${f}"
        ;;
      *)
        echo "GET ${c}/${f}: HTTP ${code}" >&2
        exit 1
        ;;
    esac
    rm -f "${tmp}"
  done
done
