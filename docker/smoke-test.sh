#!/usr/bin/env bash
# Smoke-checks a built backend image (CI runs it on every build):
#   1. `version` prints the expected commit;
#   2. `serve` answers GET /health with 200 within 10 s;
#   3. SIGTERM stops it with exit code 0 within the 15 s grace period.
#
# Usage: docker/smoke-test.sh <image> <sha>
set -euo pipefail

image=${1:?image}
sha=${2:?sha}
name=bze-scan-smoke-$$
port=18080

got=$(docker run --rm "$image" version)
if [[ "$got" != "$sha" ]]; then
	echo "version printed '$got', want '$sha'" >&2
	exit 1
fi
echo "version: $got"

# serve needs a DATABASE_URL to start; the API pool connects lazily, so an
# unreachable one is enough for /health. The indexer is off: no node here.
docker run -d --name "$name" -p "$port:8080" \
	-e HTTP_ADDR=:8080 -e INDEXER_ENABLED=false \
	-e DATABASE_URL="postgres://bze@127.0.0.1:1/none?sslmode=disable" \
	"$image" >/dev/null
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

healthy=
for _ in $(seq 1 50); do
	if curl -fsS "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
		healthy=1
		break
	fi
	sleep 0.2
done
if [[ -z "$healthy" ]]; then
	echo "GET /health did not answer 200 within 10 s" >&2
	docker logs "$name" >&2
	exit 1
fi
echo "health: 200"

start=$(date +%s)
docker stop -t 15 "$name" >/dev/null
elapsed=$(( $(date +%s) - start ))
code=$(docker inspect -f '{{.State.ExitCode}}' "$name")
if [[ "$code" != 0 || "$elapsed" -ge 15 ]]; then
	echo "SIGTERM: exit code $code after ${elapsed}s, want 0 within 15 s" >&2
	docker logs "$name" >&2
	exit 1
fi
echo "SIGTERM: exit code 0 after ${elapsed}s"
