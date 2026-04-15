#!/usr/bin/env bash
# Extract hunk line numbers with context from a git diff
# Usage: diff-hunks.sh <base-ref> <file>
# Output: "linenum: context" per hunk
# Requires: gawk

BASE="$1"
FILE="$2"

git diff "$BASE" -- "$FILE" | gawk '
/^@@/ {
  match($0, /\+([0-9]+)/, a)
  line = a[1]
  idx = index($0, "@@ ")
  rest = substr($0, idx + 3)
  idx2 = index(rest, "@@")
  if (idx2 > 0) {
    ctx = substr(rest, idx2 + 2)
    gsub(/^ +/, "", ctx)
    if (ctx != "") { print line ": " ctx; next }
  }
  waiting = 1
  current_line = line
  next
}
waiting && /^\+/ {
  content = substr($0, 2, 60)
  gsub(/^ +/, "", content)
  print current_line ": " content
  waiting = 0
  next
}
waiting && (/^@@/ || /^diff/) {
  print current_line ": line " current_line
  waiting = 0
}'
