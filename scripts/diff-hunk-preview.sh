#!/usr/bin/env bash
# Extract a single hunk from a git diff and render with delta
# Usage: diff-hunk-preview.sh <base-ref> <file> <target-line>
# Requires: gawk, delta

BASE="$1"
FILE="$2"
TARGET="$3"

git diff $BASE -- "$FILE" | gawk -v target="$TARGET" '
/^diff --git/ { diff_hdr = $0 ORS; next }
/^(index |--- |\+\+\+ )/ { diff_hdr = diff_hdr $0 ORS; next }
/^@@/ {
  if (found) exit
  match($0, /\+([0-9]+)/, a)
  if (a[1]+0 == target+0) {
    found = 1
    printf "%s", diff_hdr
    print
    next
  }
}
found { print }
' | delta
