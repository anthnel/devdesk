#!/usr/bin/env bash
# docs-only.sh tells CI whether a change touches documentation only.
#
# Usage:
#   docs-only.sh classify   NUL-separated paths on stdin
#   docs-only.sh diff       paths changed by the merge commit HEAD
#
# Contract: stdout is exactly "true" or "false" with exit code 0; "true" is the
# very last action, so any failure before it yields "false" (never a false
# green). An unknown or missing argument prints nothing and exits 2.
set -uo pipefail
export LC_ALL=C

# is_doc succeeds when the path is documentation. This is the only place in the
# repository that lists doc paths. Code exceptions come first: the first
# matching pattern wins.
is_doc() {
  case $1 in
    # Read by release-please, never by humans.
    CHANGELOG.md) return 1 ;;
    # Fixtures consumed by tests, whatever their extension.
    testdata/* | */testdata/*) return 1 ;;
    # Read by internal/command/doc_test.go.
    docs/architecture/app-shell.md) return 1 ;;
    # Documentation: top-level docs/ tree, and lowercase .md files anywhere.
    docs/*) return 0 ;;
    *.md) return 0 ;;
  esac
  return 1
}

# classify_stdin succeeds when stdin holds a non-empty NUL-separated list of
# paths that are all documentation. The last entry may lack its NUL.
classify_stdin() {
  local path count=0
  while IFS= read -r -d '' path || [[ -n $path ]]; do
    is_doc "$path" || return 1
    count=$((count + 1))
  done
  [[ $count -gt 0 ]]
}

cmd_classify() {
  if classify_stdin; then
    echo true
  else
    echo false
  fi
}

# cmd_diff classifies the files changed by HEAD when it is a merge commit.
# git output goes through a temp file: a pipe would hide git's exit status and
# command substitution would drop the NUL separators.
cmd_diff() {
  local tmp
  if ! git rev-parse -q --verify HEAD^2 >/dev/null 2>&1; then
    echo false
    return
  fi
  tmp=$(mktemp) || { echo false; return; }
  # shellcheck disable=SC2064
  trap "rm -f '$tmp'" RETURN
  if ! git diff -z --name-only --no-renames HEAD^1 HEAD >"$tmp" 2>/dev/null; then
    echo false
    return
  fi
  cmd_classify <"$tmp"
}

case ${1-} in
  classify) cmd_classify ;;
  diff) cmd_diff ;;
  *) exit 2 ;;
esac
