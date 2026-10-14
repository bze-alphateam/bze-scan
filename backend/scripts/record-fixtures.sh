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
# HEIGHTS lists heights and inclusive ranges a..b, separated by spaces.
#
# BLOCKS lists heights whose /block alone is recorded (the snapshots
# command's search for a proposal's resolution reads only block times).
#
# VALSETS lists heights whose validator set is recorded: every page of
# /validators at per_page=100 into testdata/<height>/validators.<page>.json
# (the active set at a height, BFE-192).
#
# ABCI lists proposals as <id>@<resolved height H>, the set the snapshots
# command reads (BFE-121). For each, at H - 1: the abci_query answers of bank
# Balance of the bonded_tokens_pool in ubze, gov v1 Votes and gov v1 Deposits
# (one page of 1000), into testdata/abci/<H-1>/<svc>.<Method>.<hex request>.json
# (see fakenode.ABCIFile), plus /block at H - 1 and /block_results at H. The
# requests are encoded here by hand and must stay byte for byte what the Go
# code marshals.
#
# Usage: HEIGHTS="24998316 24998321..24998330" scripts/record-fixtures.sh
#        (or: make fixtures HEIGHTS="..." [BLOCKS="..."] [ABCI="47@23821225"] [VALSETS="23821226"])
set -euo pipefail

ARCHIVE_RPC="${ARCHIVE_RPC:-https://rpc.getbze.com}"
ARCHIVE_RPC="${ARCHIVE_RPC%/}"
HEIGHTS="${HEIGHTS:-}"
BLOCKS="${BLOCKS:-}"
ABCI="${ABCI:-}"
VALSETS="${VALSETS:-}"

# The page size of /validators, CometBFT's maximum (node.ValidatorsPerPage).
PER_PAGE=100

# bonded_tokens_pool, whose ubze balance is the bonded tokens at any height.
BONDED_POOL="bze1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3gl9ytq"
PAGE_LIMIT=1000

if [[ -z "${HEIGHTS}${BLOCKS}${ABCI}${VALSETS}" ]]; then
  echo "HEIGHTS, BLOCKS, ABCI or VALSETS is required, e.g. HEIGHTS=\"24998316\"" >&2
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

expanded=""
for item in ${HEIGHTS}; do
  if [[ "${item}" =~ ^([1-9][0-9]*)\.\.([1-9][0-9]*)$ ]]; then
    lo="${BASH_REMATCH[1]}" hi="${BASH_REMATCH[2]}"
    if (( lo > hi )); then
      echo "invalid range: ${item}" >&2
      exit 2
    fi
    expanded+=" $(seq -s ' ' "${lo}" "${hi}")"
  else
    expanded+=" ${item}"
  fi
done

for h in ${expanded}; do
  if ! [[ "${h}" =~ ^[1-9][0-9]*$ ]]; then
    echo "invalid height: ${h}" >&2
    exit 2
  fi
  mkdir -p "${OUT}/${h}"
  for route in block block_results commit; do
    fetch "${route}?height=${h}" "${OUT}/${h}/${route}.json" 200
  done
done

for h in ${BLOCKS}; do
  if ! [[ "${h}" =~ ^[1-9][0-9]*$ ]]; then
    echo "invalid height: ${h}" >&2
    exit 2
  fi
  mkdir -p "${OUT}/${h}"
  fetch "block?height=${h}" "${OUT}/${h}/block.json" 200
done

for h in ${VALSETS}; do
  if ! [[ "${h}" =~ ^[1-9][0-9]*$ ]]; then
    echo "invalid height: ${h}" >&2
    exit 2
  fi
  mkdir -p "${OUT}/${h}"
  page=1 seen=0
  while :; do
    dest="${OUT}/${h}/validators.${page}.json"
    fetch "validators?height=${h}&page=${page}&per_page=${PER_PAGE}" "${dest}" 200
    total="$(grep -o '"total":"[0-9]*"' "${dest}" | grep -o '[0-9]*')"
    seen=$(( seen + $(grep -o '"count":"[0-9]*"' "${dest}" | grep -o '[0-9]*') ))
    (( seen < total )) || break
    page=$(( page + 1 ))
  done
done

# Protobuf encoding of the few requests the snapshots command sends, as hex.
varint() {
  local n="$1" out=""
  while (( n >= 128 )); do
    out+="$(printf '%02x' $(( (n & 127) | 128 )))"
    n=$(( n >> 7 ))
  done
  printf '%s%02x' "${out}" "${n}"
}
# field <number> <hex payload>: a length-delimited field.
field() {
  local payload="$2"
  printf '%02x%s%s' $(( ($1 << 3) | 2 )) "$(varint $(( ${#payload} / 2 )))" "${payload}"
}
text() { printf '%s' "$1" | od -An -tx1 -v | tr -d ' \n'; }
# QueryBalanceRequest{address, denom}
balance_req() { printf '%s%s' "$(field 1 "$(text "$1")")" "$(field 2 "$(text "$2")")"; }
# Query{Votes,Deposits}Request{proposal_id, pagination: {limit}}
by_proposal_req() { printf '08%s%s' "$(varint "$1")" "$(field 2 "18$(varint "${PAGE_LIMIT}")")"; }

# abci <query path> <hex request> <height>
abci() {
  local qpath="$1" data="$2" h="$3" file
  file="${qpath#/}"
  file="${file//\//.}"
  mkdir -p "${OUT}/abci/${h}"
  fetch "abci_query?path=%22${qpath}%22&data=0x${data}&height=${h}" "${OUT}/abci/${h}/${file}.${data}.json" 200
  if grep -q '"code":[1-9]' "${OUT}/abci/${h}/${file}.${data}.json"; then
    echo "abci_query ${qpath} at ${h} answered an error" >&2
    exit 1
  fi
}

for item in ${ABCI}; do
  if ! [[ "${item}" =~ ^([1-9][0-9]*)@([1-9][0-9]*)$ ]]; then
    echo "invalid ABCI item (want <id>@<resolved height>): ${item}" >&2
    exit 2
  fi
  id="${BASH_REMATCH[1]}" h="${BASH_REMATCH[2]}"
  at=$(( h - 1 ))
  abci /cosmos.bank.v1beta1.Query/Balance "$(balance_req "${BONDED_POOL}" ubze)" "${at}"
  abci /cosmos.gov.v1.Query/Votes "$(by_proposal_req "${id}")" "${at}"
  abci /cosmos.gov.v1.Query/Deposits "$(by_proposal_req "${id}")" "${at}"
  mkdir -p "${OUT}/${at}" "${OUT}/${h}"
  fetch "block?height=${at}" "${OUT}/${at}/block.json" 200
  fetch "block_results?height=${h}" "${OUT}/${h}/block_results.json" 200
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
