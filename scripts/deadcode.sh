#!/usr/bin/env bash
# Report unreachable functions and fail on new ones.
#
# `unused` (in golangci-lint) only sees within a package, and it assumes every
# exported identifier is API. It cannot tell that an exported function is
# reachable from no main and no test. deadcode does whole-program reachability
# analysis from main, so it catches the code an agent leaves behind after a
# refactor: a helper that nothing calls any more.
#
# The tree has known unreachable functions, mostly unused widgets. Failing on
# all of them would make the check useless on day one, so the known set lives
# in scripts/deadcode-baseline.txt. New dead code fails; the baseline is a
# to-do list, not a permanent exemption.
#
# Usage:
#   scripts/deadcode.sh           report new dead code, fail if any
#   scripts/deadcode.sh --update  rewrite the baseline after deleting code
#   scripts/deadcode.sh --list    print every finding, baseline included
set -euo pipefail

cd "$(dirname "$0")/.."
BASELINE="scripts/deadcode-baseline.txt"

if ! command -v deadcode >/dev/null 2>&1; then
	echo "deadcode not found. Install it with:" >&2
	echo "  go install golang.org/x/tools/cmd/deadcode@latest" >&2
	exit 127
fi

# -test counts functions reachable from tests as live. Without it, every
# test-only helper is a false positive.
#
# Strip the line and column, keeping "<file> <func>". A finding must survive
# edits above it, or the baseline churns on every unrelated change.
current=$(deadcode -test ./... 2>/dev/null |
	sed 's/^\([^:]*\):[0-9]*:[0-9]*: unreachable func: /\1 /' |
	sort)

case "${1:-}" in
--list)
	echo "$current"
	exit 0
	;;
--update)
	printf '%s\n' "$current" >"$BASELINE"
	echo "baseline updated: $(wc -l <"$BASELINE" | tr -d ' ') entries"
	exit 0
	;;
esac

[ -f "$BASELINE" ] || : >"$BASELINE"

# Findings not in the baseline.
new=$(comm -13 "$BASELINE" <(printf '%s\n' "$current") || true)
# Baseline entries that no longer appear, so the baseline can shrink.
fixed=$(comm -23 "$BASELINE" <(printf '%s\n' "$current") || true)

if [ -n "$fixed" ]; then
	echo "Dead code removed. Run 'scripts/deadcode.sh --update' to shrink the baseline:"
	printf '%s\n' "$fixed" | awk '{print "  " $2}'
	echo
fi

if [ -n "$new" ]; then
	echo "New unreachable functions found:"
	printf '%s\n' "$new" | while read -r file fn; do
		echo "  $file: $fn"
	done
	echo
	echo "Nothing reaches these from main or from a test. Delete them, or call"
	echo "them. If a function is kept on purpose, add it with:"
	echo "  scripts/deadcode.sh --update"
	exit 1
fi

echo "No new dead code ($(wc -l <"$BASELINE" | tr -d ' ') known, tracked in $BASELINE)."
