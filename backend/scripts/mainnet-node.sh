#!/usr/bin/env bash
# A BZE mainnet node for the soak (docs/soak.md): built from source, state
# synced, its CometBFT psql indexer writing into the development PostgreSQL.
# Everything lives under backend/.local (git-ignored). Never a container image.
#
#   build   clones github.com/bze-alphateam/bze at the tag mainnet runs (the
#           version TRUST_RPC's /abci_info reports, or BZED_VERSION) into
#           .local/bze and builds bzed into .local/bin, with the Go line
#           the tag's Makefile requires (GOTOOLCHAIN, or GO_TOOLCHAIN=go1.x.y)
#   init    creates the home .local/bzed-home: the chain's mainnet genesis
#           (genesis.json of the cloned tag, the 2022 launch file: state sync
#           restores a recent snapshot and never replays it), seeds and persistent peers from
#           the chain registry's chain.json, state sync with a trust height
#           2000 blocks below TRUST_RPC's tip, the psql indexer, RPC and gRPC
#           on localhost only
#   start   runs bzed in the foreground; refuses unless SOAK=1
#   reset   deletes the home (the database is not touched)
#
# Only /status, /abci_info and /block?height= are called on public nodes;
# never a search route.
#
# Usage: scripts/mainnet-node.sh build|init|start|reset
#        (or: make node-build node-init, SOAK=1 make node-start, make node-reset)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCAL="${ROOT}/.local"
SRC="${LOCAL}/bze"
BIN="${LOCAL}/bin/bzed"
HOME_DIR="${LOCAL}/bzed-home"

CHAIN_ID="beezee-1"
BZE_REPO="${BZE_REPO:-https://github.com/bze-alphateam/bze.git}"
BZED_VERSION="${BZED_VERSION:-}"
TRUST_RPC="${TRUST_RPC:-https://rpc.getbze.com}"
TRUST_RPC="${TRUST_RPC%/}"
TRUST_OFFSET="${TRUST_OFFSET:-2000}"
STATESYNC_RPC_SERVERS="${STATESYNC_RPC_SERVERS:-https://rpc.getbze.com:443,https://rpc-1.getbze.com:443}"
CHAIN_JSON_URL="${CHAIN_JSON_URL:-https://raw.githubusercontent.com/cosmos/chain-registry/master/beezee/chain.json}"
# The psql sink's database: the compose PostgreSQL unless DATABASE_URL is set.
DATABASE_URL="${DATABASE_URL:-postgres://bze:bze@localhost:15432/bze_index?sslmode=disable}"
MONIKER="${MONIKER:-bze-scan-soak}"

die() {
  echo "mainnet-node: $*" >&2
  exit 1
}

need() {
  local c
  for c in "$@"; do
    command -v "${c}" >/dev/null 2>&1 || die "${c} is required"
  done
}

# rpc <route-with-query>: GET on TRUST_RPC, the JSON-RPC result on stdout.
rpc() {
  curl -fsS -m 30 "${TRUST_RPC}/$1" | jq -e '.result'
}

# set_toml <file> <section> <key> <value>: replaces the value of key in
# [section] ("" for the top level), the value written as given (quote strings
# yourself). Fails when the key is missing, so a changed template is noticed.
set_toml() {
  local file="$1" section="$2" key="$3" value="$4" tmp
  tmp="$(mktemp)"
  if ! awk -v section="${section}" -v key="${key}" -v value="${value}" '
    /^[[:space:]]*\[[^]]+\][[:space:]]*$/ {
      current = $0
      gsub(/^[[:space:]]*\[|\][[:space:]]*$/, "", current)
    }
    current == section && $0 ~ "^[[:space:]]*" key "[[:space:]]*=" && !done {
      print key " = " value
      done = 1
      next
    }
    { print }
    END { exit done ? 0 : 1 }
  ' "${file}" >"${tmp}"; then
    rm -f "${tmp}"
    die "${file}: no key ${key} in [${section}]"
  fi
  mv "${tmp}" "${file}"
}

# quoted <string>: the string as a TOML basic string.
quoted() {
  printf '"%s"' "$1"
}

cmd_build() {
  need git go curl jq make
  local version="${BZED_VERSION}"
  if [[ -z "${version}" ]]; then
    version="v$(rpc abci_info | jq -r '.response.version')"
    [[ "${version}" != "v" && "${version}" != "vnull" ]] || die "no version in ${TRUST_RPC}/abci_info"
  fi
  echo "building bzed ${version} (the version mainnet runs; set BZED_VERSION to override)"
  if [[ -d "${SRC}/.git" ]]; then
    git -C "${SRC}" fetch --quiet --tags origin
  else
    mkdir -p "${LOCAL}"
    git clone --quiet "${BZE_REPO}" "${SRC}"
  fi
  git -C "${SRC}" -c advice.detachedHead=false checkout --quiet "refs/tags/${version}"
  # The chain's Makefile refuses any Go line but the one it was released
  # with (consensus safety): build with that line's latest patch.
  local line toolchain="${GO_TOOLCHAIN:-}"
  line="$(grep -oE 'go1\.[0-9]+' "${SRC}/Makefile" | head -1 || true)"
  if [[ -z "${toolchain}" && -n "${line}" ]] && ! go version | grep -q "${line}[. ]"; then
    toolchain="$(curl -fsS -m 30 'https://go.dev/dl/?mode=json&include=all' |
      jq -r --arg p "${line}." '[.[].version | select(startswith($p))][0] // empty')"
    [[ -n "${toolchain}" ]] || die "no ${line} release found on go.dev; set GO_TOOLCHAIN"
  fi
  echo "with ${toolchain:-the local Go}"
  mkdir -p "$(dirname "${BIN}")"
  GOTOOLCHAIN="${toolchain:-${GOTOOLCHAIN:-auto}}" \
    make -C "${SRC}" build NETWORK=mainnet LEDGER_ENABLED=false BUILDDIR="$(dirname "${BIN}")"
  "${BIN}" version
}

cmd_init() {
  need curl jq
  [[ -x "${BIN}" ]] || die "no bzed at ${BIN}: run make node-build first"
  # The sink writes into the development database only.
  local db_host
  db_host="$(sed -E 's#^[a-z]+://([^@/]*@)?(\[[^]]*\]|[^:/?]*).*#\2#' <<<"${DATABASE_URL}")"
  case "${db_host}" in
    localhost | 127.0.0.1 | "[::1]") ;;
    *) die "DATABASE_URL must point at the local development PostgreSQL (host ${db_host})" ;;
  esac
  [[ ! -e "${HOME_DIR}" ]] || die "${HOME_DIR} exists: run make node-reset first"

  local genesis="${SRC}/genesis.json"
  [[ -f "${genesis}" ]] || die "no ${genesis}: run make node-build first"
  [[ "$(jq -r '.chain_id' "${genesis}")" == "${CHAIN_ID}" ]] || die "${genesis} is not ${CHAIN_ID}"

  local chain_json seeds peers min_gas
  chain_json="$(curl -fsS -m 30 "${CHAIN_JSON_URL}")"
  seeds="$(jq -r '[.peers.seeds[] | "\(.id)@\(.address)"] | join(",")' <<<"${chain_json}")"
  peers="$(jq -r '[.peers.persistent_peers[] | "\(.id)@\(.address)"] | join(",")' <<<"${chain_json}")"
  min_gas="$(jq -r '.fees.fee_tokens[] | select(.denom == "ubze") | "\(.fixed_min_gas_price)ubze"' <<<"${chain_json}")"
  [[ -n "${seeds}${peers}" ]] || die "no peers in ${CHAIN_JSON_URL}"

  local latest trust_height trust_hash
  latest="$(rpc status | jq -r '.sync_info.latest_block_height')"
  trust_height=$((latest - TRUST_OFFSET))
  trust_hash="$(rpc "block?height=${trust_height}" | jq -r '.block_id.hash')"
  [[ "${trust_hash}" =~ ^[0-9A-F]{64}$ ]] || die "bad trust hash ${trust_hash}"

  "${BIN}" init "${MONIKER}" --chain-id "${CHAIN_ID}" --home "${HOME_DIR}" >/dev/null 2>&1 || die "bzed init failed"
  cp "${genesis}" "${HOME_DIR}/config/genesis.json"

  local config="${HOME_DIR}/config/config.toml" app="${HOME_DIR}/config/app.toml"
  set_toml "${config}" "p2p" "seeds" "$(quoted "${seeds}")"
  set_toml "${config}" "p2p" "persistent_peers" "$(quoted "${peers}")"
  set_toml "${config}" "rpc" "laddr" "$(quoted "tcp://127.0.0.1:26657")"
  set_toml "${config}" "statesync" "enable" "true"
  set_toml "${config}" "statesync" "rpc_servers" "$(quoted "${STATESYNC_RPC_SERVERS}")"
  set_toml "${config}" "statesync" "trust_height" "${trust_height}"
  set_toml "${config}" "statesync" "trust_hash" "$(quoted "${trust_hash}")"
  set_toml "${config}" "storage" "discard_abci_responses" "false"
  set_toml "${config}" "tx_index" "indexer" "$(quoted "psql")"
  set_toml "${config}" "tx_index" "psql-conn" "$(quoted "${DATABASE_URL}")"
  set_toml "${app}" "" "minimum-gas-prices" "$(quoted "${min_gas:-0.01ubze}")"
  set_toml "${app}" "grpc" "enable" "true"
  set_toml "${app}" "grpc" "address" "$(quoted "127.0.0.1:9090")"
  set_toml "${app}" "api" "enable" "false"

  echo "initialised ${HOME_DIR}: trust height ${trust_height} (${trust_hash}), psql sink on ${db_host}"
}

cmd_start() {
  [[ "${SOAK:-}" == "1" ]] || die "refusing to start a node without SOAK=1 (see docs/soak.md)"
  [[ -x "${BIN}" ]] || die "no bzed at ${BIN}: run make node-build first"
  [[ -f "${HOME_DIR}/config/config.toml" ]] || die "no home at ${HOME_DIR}: run make node-init first"
  exec "${BIN}" start --home "${HOME_DIR}"
}

cmd_reset() {
  rm -rf "${HOME_DIR}"
  echo "removed ${HOME_DIR}; the database is untouched (see docs/soak.md to reset it)"
}

case "${1:-}" in
  build) cmd_build ;;
  init) cmd_init ;;
  start) cmd_start ;;
  reset) cmd_reset ;;
  *)
    echo "usage: $0 build|init|start|reset" >&2
    exit 2
    ;;
esac
