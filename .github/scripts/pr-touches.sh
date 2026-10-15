#!/usr/bin/env bash
# pr-touches.sh <pattern>... — does this pull request change a matching path?
#
# Required checks and path filters do not mix: a workflow whose trigger is
# path-filtered never starts on a pull request outside its paths, so the
# required check never reports and the pull request stays blocked. The
# required workflows therefore start on every pull request, and their first
# job runs this script; the real job runs only when it prints run=true and is
# otherwise skipped, which GitHub counts as a passing required check.
#
# Each pattern is an extended regex matched against the changed paths
# (`git diff --name-only BASE...HEAD`, i.e. since the merge base), e.g.
#   pr-touches.sh '^backend/' '^\.github/workflows/backend-unit\.yml$'
#
# Needs BASE and HEAD (the pull request's base and head SHAs) and a checkout
# with enough history to find their merge base (fetch-depth: 0). Writes
# run=true|false to $GITHUB_OUTPUT (stdout when unset) and lists the matches.
set -euo pipefail

(($# > 0)) || { echo "usage: $0 <pattern>..." >&2; exit 2; }
: "${BASE:?BASE (base SHA) is required}" "${HEAD:?HEAD (head SHA) is required}"

regex=$(IFS='|'; echo "$*")
changed=$(git diff --name-only "$BASE...$HEAD")
matches=$(grep -E "$regex" <<<"$changed" || true)

if [[ -n "$matches" ]]; then
  run=true
  echo "Changed paths matching ($regex):"
  sed 's/^/  /' <<<"$matches"
else
  run=false
  echo "No changed path matches ($regex): the check is skipped."
fi
echo "run=$run" >>"${GITHUB_OUTPUT:-/dev/stdout}"
