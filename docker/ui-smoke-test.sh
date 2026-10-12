#!/usr/bin/env bash
# Smoke-checks a built UI image (CI runs it on every build):
#   1. the revision label holds the expected commit;
#   2. the server answers GET / with 200 within 15 s;
#   3. SIGTERM stops it with exit code 0 within the 10 s grace period.
#
# Usage: docker/ui-smoke-test.sh <image> <sha>
set -euo pipefail

image=${1:?image}
sha=${2:?sha}
name=bze-scan-ui-smoke-$$
port=13000

got=$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
if [[ "$got" != "$sha" ]]; then
	echo "revision label is '$got', want '$sha'" >&2
	exit 1
fi
echo "revision: $got"

# The home page needs no API: no backend here.
docker run -d --name "$name" -p "$port:3000" "$image" >/dev/null
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

up=
for _ in $(seq 1 75); do
	if curl -fsS -o /dev/null "http://127.0.0.1:$port/" 2>/dev/null; then
		up=1
		break
	fi
	sleep 0.2
done
if [[ -z "$up" ]]; then
	echo "GET / did not answer 200 within 15 s" >&2
	docker logs "$name" >&2
	exit 1
fi
echo "GET /: 200"

start=$(date +%s)
docker stop -t 10 "$name" >/dev/null
elapsed=$(( $(date +%s) - start ))
code=$(docker inspect -f '{{.State.ExitCode}}' "$name")
if [[ "$code" != 0 || "$elapsed" -ge 10 ]]; then
	echo "SIGTERM: exit code $code after ${elapsed}s, want 0 within 10 s" >&2
	docker logs "$name" >&2
	exit 1
fi
echo "SIGTERM: exit code 0 after ${elapsed}s"
