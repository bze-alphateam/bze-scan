#!/usr/bin/env bash
# Records CometBFT RPC fixtures for the fake node used by the tests
# (internal/testutil/fakenode/testdata).
#
# Fixtures are committed to the repository and re-recorded only on purpose:
# a new height when a story needs a message type or event format the existing
# fixtures do not cover, never as a routine refresh.
#
# For every height in HEIGHTS it fetches /block, /block_results and /commit
# once each from ARCHIVE_RPC into testdata/<height>/. It also records /status
# into testdata/status.json and one answer for a height above the node's tip
# into testdata/above_tip.json. Responses are stored verbatim, as the node
# returned them.
#
# Only by-height routes are ever called. Never /tx, /tx_search or
# /block_search, on any node.
#
# Usage: HEIGHTS="24998316 24998321" scripts/record-fixtures.sh
#        (or: make fixtures HEIGHTS="...")
set -euo pipefail

ARCHIVE_RPC="${ARCHIVE_RPC:-https://rpc.getbze.com}"
ARCHIVE_RPC="${ARCHIVE_RPC%/}"
HEIGHTS="${HEIGHTS:-}"

if [[ -z "${HEIGHTS}" ]]; then
  echo "HEIGHTS is required, e.g. HEIGHTS=\"24998316\"" >&2
  exit 2
fi

OUT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/internal/testutil/fakenode/testdata"
mkdir -p "${OUT}"

# fetch <route-with-query> <destination> <expected-http-status>
fetch() {
  local route="$1" dest="$2" want="$3" tmp code
  tmp="$(mktemp)"
  code="$(curl -sS -o "${tmp}" -w '%{http_code}' "${ARCHIVE_RPC}/${route}")"
  if [[ "${code}" != "${want}" ]]; then
    echo "GET ${route}: HTTP ${code}, want ${want}" >&2
    cat "${tmp}" >&2
    rm -f "${tmp}"
    exit 1
  fi
  mv "${tmp}" "${dest}"
  chmod 644 "${dest}"
  echo "recorded ${route} -> ${dest#"${OUT}/"}"
}

for h in ${HEIGHTS}; do
  if ! [[ "${h}" =~ ^[1-9][0-9]*$ ]]; then
    echo "invalid height: ${h}" >&2
    exit 2
  fi
  mkdir -p "${OUT}/${h}"
  for route in block block_results commit; do
    fetch "${route}?height=${h}" "${OUT}/${h}/${route}.json" 200
  done
done

fetch "status" "${OUT}/status.json" 200

# A real node answers every by-height route above its tip with the same
# JSON-RPC error (HTTP 500); the fake node replays it for heights it has no
# fixture for.
tip="$(sed -n 's/.*"latest_block_height":"\([0-9]*\)".*/\1/p' "${OUT}/status.json")"
if [[ -z "${tip}" ]]; then
  echo "could not read latest_block_height from status.json" >&2
  exit 1
fi
fetch "block?height=$((tip + 1000000))" "${OUT}/above_tip.json" 500
