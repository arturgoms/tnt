#!/usr/bin/env bash
# Layout: ai — opencode full-width + small shell below
# ┌────────────────────┐
# │     opencode       │
# ├────────────────────┤
# │      shell         │
# └────────────────────┘
# Args: $1=workdir $2=session $3=branch [after_wid] [color]
# Requires: opencode (integrations.opencode = true)

set -e

WORKDIR="$1"
SESSION="$2"
BRANCH="$3"
AFTER_WID="${4:-}"
COLOR="${5:-}"

[[ -z "$WORKDIR" || -z "$SESSION" || -z "$BRANCH" ]] && {
  echo "Usage: ai.sh <workdir> <session> <branch> [after_wid] [color]"; exit 1
}

if [[ -n "$AFTER_WID" ]]; then
  WID=$(tmux new-window -P -F '#{window_id}' -a -t "$AFTER_WID" -n "${BRANCH##*/}:ai" -c "$WORKDIR")
else
  WID=$(tmux new-window -P -F '#{window_id}' -t "$SESSION" -n "${BRANCH##*/}:ai" -c "$WORKDIR")
fi
tmux set-option -w -t "$WID" @worktree "$BRANCH"
[[ -n "$COLOR" ]] && tmux set-option -w -t "$WID" @worktree_color "$COLOR"

tmux send-keys -t "$WID" "opencode --port" Enter
tmux split-window -t "$WID" -v -l 25% -c "$WORKDIR"
tmux select-pane -t "$WID.1"
