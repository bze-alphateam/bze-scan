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
# COMMITS lists heights and ranges a..b whose /commit alone is recorded (the
# upgrade recovery reads who signed the blocks after an upgrade, BFE-194).
#
# CONSENSUS_STATE=1 records /consensus_state into
# testdata/consensus_state.json: the votes of the round in progress, asked
# until an answer holds at least 8 votes (most answers come between rounds,
# all nil-Vote). It is a live route, not by height: the halt watch reads it
# from the local node only.
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
# ORDERS lists owners as <address>@<height>, the open orders route's archive
# fallback (BFE-250). At that height: the abci_query answers of tradebin
# AllMarkets and UserMarketOrders (one page of 100 each, the Go code's page
# size) and MarketOrder of every reference, the references read from the
# REST gateway (REST) pinned to the same height. Record the gRPC fixtures at
# the same height with make grpc-fixtures SETS=orders OWNERS=… ORDERS_HEIGHT=….
#
# Usage: HEIGHTS="24998316 24998321..24998330" scripts/record-fixtures.sh
#        (or: make fixtures HEIGHTS="..." [BLOCKS="..."] [COMMITS="a..b"] [ABCI="47@23821225"] [VALSETS="23821226"] [CONSENSUS_STATE=1] [ORDERS="bze1…@25159796"])
set -euo pipefail

ARCHIVE_RPC="${ARCHIVE_RPC:-https://rpc.getbze.com}"
ARCHIVE_RPC="${ARCHIVE_RPC%/}"
HEIGHTS="${HEIGHTS:-}"
BLOCKS="${BLOCKS:-}"
ABCI="${ABCI:-}"
ORDERS="${ORDERS:-}"
REST="${REST:-https://rest.getbze.com}"
REST="${REST%/}"
VALSETS="${VALSETS:-}"
COMMITS="${COMMITS:-}"
CONSENSUS_STATE="${CONSENSUS_STATE:-}"

# The page size of /validators, CometBFT's maximum (node.ValidatorsPerPage).
PER_PAGE=100

# bonded_tokens_pool, whose ubze balance is the bonded tokens at any height.
BONDED_POOL="bze1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3gl9ytq"
PAGE_LIMIT=1000

if [[ -z "${HEIGHTS}${BLOCKS}${ABCI}${VALSETS}${COMMITS}${CONSENSUS_STATE}${ORDERS}" ]]; then
  echo "HEIGHTS, BLOCKS, COMMITS, ABCI, VALSETS, CONSENSUS_STATE or ORDERS is required, e.g. HEIGHTS=\"24998316\"" >&2
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

# expand <heights and ranges a..b>: every height, space-separated.
expand() {
  local item lo hi out=""
  for item in $1; do
    if [[ "${item}" =~ ^([1-9][0-9]*)\.\.([1-9][0-9]*)$ ]]; then
      lo="${BASH_REMATCH[1]}" hi="${BASH_REMATCH[2]}"
      if (( lo > hi )); then
        echo "invalid range: ${item}" >&2
        exit 2
      fi
      out+=" $(seq -s ' ' "${lo}" "${hi}")"
    else
      out+=" ${item}"
    fi
  done
  echo "${out}"
}
expanded="$(expand "${HEIGHTS}")"
commits="$(expand "${COMMITS}")"

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

for h in ${commits}; do
  if ! [[ "${h}" =~ ^[1-9][0-9]*$ ]]; then
    echo "invalid height: ${h}" >&2
    exit 2
  fi
  mkdir -p "${OUT}/${h}"
  fetch "commit?height=${h}" "${OUT}/${h}/commit.json" 200
done

if [[ -n "${CONSENSUS_STATE}" ]]; then
  for _ in $(seq 1 100); do
    fetch "consensus_state" "${OUT}/consensus_state.json" 200
    if (( $(grep -o 'Vote{' "${OUT}/consensus_state.json" | wc -l) >= 8 )); then
      break
    fi
  done
fi

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

# PageRequest{limit: 100}, the open orders route's page size
# (openorders.PageLimit).
page100() { printf '18%s' "$(varint 100)"; }
# QueryAllMarketsRequest{pagination}
all_markets_req() { field 1 "$(page100)"; }
# QueryUserMarketOrdersRequest{address, pagination}
user_orders_req() { printf '%s%s' "$(field 1 "$(text "$1")")" "$(field 3 "$(page100)")"; }
# QueryMarketOrderRequest{market, order_type, order_id}
market_order_req() { printf '%s%s%s' "$(field 1 "$(text "$1")")" "$(field 2 "$(text "$2")")" "$(field 3 "$(text "$3")")"; }

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

for item in ${ORDERS}; do
  if ! [[ "${item}" =~ ^(bze1[0-9a-z]+)@([1-9][0-9]*)$ ]]; then
    echo "invalid ORDERS item (want <address>@<height>): ${item}" >&2
    exit 2
  fi
  owner="${BASH_REMATCH[1]}" h="${BASH_REMATCH[2]}"
  abci /bze.tradebin.Query/AllMarkets "$(all_markets_req)" "${h}"
  abci /bze.tradebin.Query/UserMarketOrders "$(user_orders_req "${owner}")" "${h}"
  refs="$(curl -sS -H "x-cosmos-block-height: ${h}" \
    "${REST}/bze/tradebin/user_market_orders/${owner}?pagination.limit=100")"
  if [[ "$(jq -r '.pagination.next_key // empty' <<<"${refs}")" != "" ]]; then
    echo "${owner} has more than 100 orders at ${h}: later pages are not recorded" >&2
    exit 1
  fi
  while IFS=$'\t' read -r market side id; do
    abci /bze.tradebin.Query/MarketOrder "$(market_order_req "${market}" "${side}" "${id}")" "${h}"
  done < <(jq -r '.list[] | [.market_id, .order_type, .id] | @tsv' <<<"${refs}")
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
