#!/usr/bin/env bash
# Git diff viewer — commit picker + file browser with fzf + delta
# Usage: diff-view.sh [workdir] [branch-name]
# Requires: fzf, delta, nvim (optional)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

WORKDIR="${1:-$PWD}"
cd "$WORKDIR"
BRANCH="${2:-$(git branch --show-current 2>/dev/null || echo "workspace")}"

NVIM_PANE=""
ORIGIN_WINDOW=$(tmux display-message -p '#{window_id}' 2>/dev/null || true)
ORIGIN_PANE_CMD=$(tmux display-message -p '#{pane_current_command}' 2>/dev/null || true)
if [[ "$ORIGIN_PANE_CMD" == "nvim" || "$ORIGIN_PANE_CMD" == "vim" ]]; then
  NVIM_PANE=$(tmux display-message -p '#{pane_id}' 2>/dev/null || true)
fi

open_in_editor() {
  local file="$1"
  local line="${2:-1}"

  if [[ -n "$NVIM_PANE" ]]; then
    tmux send-keys -t "$NVIM_PANE" Escape Escape
    tmux send-keys -t "$NVIM_PANE" ":edit +${line} ${WORKDIR}/${file}" Enter
    tmux select-window -t "$ORIGIN_WINDOW" 2>/dev/null
    tmux select-pane -t "$NVIM_PANE" 2>/dev/null
  else
    tmux new-window -a -n "$BRANCH:edit" -c "$WORKDIR" "nvim +${line} '$file'"
  fi
}

BASE_BRANCH=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's|refs/remotes/origin/||' || echo "")
[[ -z "$BASE_BRANCH" ]] && BASE_BRANCH=$(git branch -l main master develop 2>/dev/null | head -1 | tr -d '* ' || echo "master")

MERGE_BASE=$(git merge-base HEAD "$BASE_BRANCH" 2>/dev/null || echo "")
[[ -z "$MERGE_BASE" ]] && { tmux display-message "Cannot find merge-base with $BASE_BRANCH"; exit 0; }

ON_BASE_BRANCH=false
if [[ "$(git rev-parse HEAD)" == "$(git rev-parse "$BASE_BRANCH" 2>/dev/null)" ]]; then
  ON_BASE_BRANCH=true
fi

build_commit_list() {
  local has_uncommitted=false
  if [[ -n "$(git diff --name-only 2>/dev/null)" ]] || [[ -n "$(git diff --cached --name-only 2>/dev/null)" ]]; then
    has_uncommitted=true
  fi

  local commits
  if [[ "$ON_BASE_BRANCH" == "true" ]]; then
    commits=$(git log --format="%h	%ar	%s" -20 HEAD 2>/dev/null || true)
  else
    commits=$(git log --format="%h	%ar	%s" "$MERGE_BASE..HEAD" 2>/dev/null || true)
  fi

  if [[ "$has_uncommitted" == "false" ]] && [[ -z "$commits" ]]; then
    return 1
  fi

  if [[ "$has_uncommitted" == "true" ]]; then
    echo "uncommitted	 uncommitted changes"
  fi

  if [[ -n "$commits" ]]; then
    echo "$commits" | while IFS=$'\t' read -r hash date msg; do
      printf '%s\t%s  %-14s %s\n' "$hash" "$hash" "$date" "$msg"
    done
  fi
}

while true; do
  commit_list=$(build_commit_list) || { tmux display-message "No changes vs $BASE_BRANCH"; exit 0; }

  entry_count=$(echo "$commit_list" | wc -l | tr -d ' ')
  if [[ "$entry_count" -eq 1 ]]; then
    selected_key=$(echo "$commit_list" | head -1 | cut -d$'\t' -f1)
  else
    selected=$(echo "$commit_list" | fzf \
      --delimiter=$'\t' --with-nth=2 \
      --header "$(if [[ "$ON_BASE_BRANCH" == "true" ]]; then echo "recent commits on $BASE_BRANCH"; else echo "diff against $BASE_BRANCH"; fi) (newest first) | esc: quit" \
      --no-multi \
      --preview 'hash={1}; if [[ "$hash" == "uncommitted" ]]; then git diff --stat HEAD; elif [[ "'"$ON_BASE_BRANCH"'" == "true" ]]; then git diff --stat {1}~1..{1}; else git diff --stat '"$MERGE_BASE"'..{1}; fi' \
      --preview-window right:50%) || exit 0

    selected_key=$(echo "$selected" | cut -d$'\t' -f1)
  fi

  if [[ "$selected_key" == "uncommitted" ]]; then
    DIFF_CMD="git diff HEAD"
    DIFF_FILES_CMD="git diff --name-only HEAD"
    HEADER_LABEL="uncommitted changes"
  elif [[ "$ON_BASE_BRANCH" == "true" ]]; then
    DIFF_CMD="git diff ${selected_key}~1..$selected_key"
    DIFF_FILES_CMD="git diff --name-only ${selected_key}~1..$selected_key"
    HEADER_LABEL="$selected_key"
  else
    DIFF_CMD="git diff $MERGE_BASE..$selected_key"
    DIFF_FILES_CMD="git diff --name-only $MERGE_BASE..$selected_key"
    HEADER_LABEL="$selected_key vs $BASE_BRANCH"
  fi

  files=$(eval "$DIFF_FILES_CMD")
  if [[ -z "$files" ]]; then
    tmux display-message "No file changes for $HEADER_LABEL"
    continue
  fi

  if [[ "$selected_key" == "uncommitted" ]]; then
    HUNK_BASE="HEAD"
    HUNK_DIFF_CMD="git diff HEAD"
  elif [[ "$ON_BASE_BRANCH" == "true" ]]; then
    HUNK_BASE="${selected_key}~1..$selected_key"
    HUNK_DIFF_CMD="git diff ${selected_key}~1..$selected_key"
  else
    HUNK_BASE="$MERGE_BASE..$selected_key"
    HUNK_DIFF_CMD="git diff $MERGE_BASE..$selected_key"
  fi

  last_pos=1
  back_to_commits=false

  while true; do
    result=$(echo "$files" | fzf \
      --preview "$DIFF_CMD -- {} | delta" \
      --preview-window right:70% \
      --header "$HEADER_LABEL | enter: open | →: hunks | esc: back" \
      --bind "start:pos($last_pos)" \
      --bind "ctrl-r:reload($DIFF_FILES_CMD)+refresh-preview" \
      --expect=right,enter) || { back_to_commits=true; break; }

    key=$(echo "$result" | head -1)
    file=$(echo "$result" | tail -1)

    [[ -z "$file" ]] && { back_to_commits=true; break; }

    last_pos=$(echo "$files" | grep -n "^${file}$" | head -1 | cut -d: -f1)

    if [[ "$key" == "right" ]]; then
      sel=$(bash "$SCRIPT_DIR/diff-hunks.sh" "$HUNK_BASE" "$file" | \
        fzf --prompt "hunk ($file)> " \
          --preview 'bash "'"$SCRIPT_DIR"'/diff-hunk-preview.sh" "'"$HUNK_BASE"'" "'"$file"'" $(echo {} | cut -d: -f1 | tr -d " ")' \
          --preview-window right:70% \
          --header "enter: open at line | esc: back" \
          --bind "ctrl-r:reload(bash \"$SCRIPT_DIR/diff-hunks.sh\" \"$HUNK_BASE\" \"$file\")+refresh-preview" \
          --expect=enter) || continue

      hunk_line=$(echo "$sel" | tail -1)
      num=$(echo "$hunk_line" | cut -d: -f1 | tr -d ' ')
      [[ -n "$num" ]] && open_in_editor "$file" "$num"
      exit 0
    fi

    line=$(bash "$SCRIPT_DIR/diff-hunks.sh" "$HUNK_BASE" "$file" | head -1 | cut -d: -f1 | tr -d ' ')
    open_in_editor "$file" "${line:-1}"
    exit 0
  done

  [[ "$back_to_commits" == "true" ]] && continue
done
