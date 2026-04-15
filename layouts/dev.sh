#!/usr/bin/env bash
# Layout: dev — nvim (main) + opencode (right) + shell (bottom-right)
# ┌──────────┬──────────┐
# │          │ opencode │
# │   nvim   ├──────────┤
# │          │  shell   │
# └──────────┴──────────┘
# Args: $1=workdir $2=session $3=branch [after_wid] [color]
# Requires: nvim, opencode (integrations.opencode = true)

set -e

WORKDIR="$1"
SESSION="$2"
BRANCH="$3"
AFTER_WID="${4:-}"
COLOR="${5:-}"
TNT_DIR="${HOME}/.config/tnt"

[[ -z "$WORKDIR" || -z "$SESSION" || -z "$BRANCH" ]] && {
  echo "Usage: dev.sh <workdir> <session> <branch> [after_wid] [color]"; exit 1
}

SOCKET_NAME=$(echo "${SESSION}_${BRANCH##*/}" | tr '/' '_')
SOCKET_DIR="$TNT_DIR/projects/$SESSION/sockets"
SOCKET_PATH="$SOCKET_DIR/${SOCKET_NAME}.sock"
mkdir -p "$SOCKET_DIR"

if [[ -n "$AFTER_WID" ]]; then
  WID=$(tmux new-window -P -F '#{window_id}' -a -t "$AFTER_WID" -n "${BRANCH##*/}:dev" -c "$WORKDIR")
else
  WID=$(tmux new-window -P -F '#{window_id}' -t "$SESSION" -n "${BRANCH##*/}:dev" -c "$WORKDIR")
fi
tmux set-option -w -t "$WID" @worktree "$BRANCH"
[[ -n "$COLOR" ]] && tmux set-option -w -t "$WID" @worktree_color "$COLOR"

tmux send-keys -t "$WID" "nvim --listen '$SOCKET_PATH' ." Enter
tmux split-window -t "$WID" -h -l 40% -c "$WORKDIR"
tmux send-keys "NVIM_SOCKET_PATH='$SOCKET_PATH' opencode --port" Enter
tmux split-window -v -l 30% -c "$WORKDIR"
tmux select-pane -t "$WID.1"
