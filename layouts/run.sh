#!/usr/bin/env bash
# Layout: run — one pane per service from projects/{repo}/config.json
# ┌────────┬────────┬────────┐
# │ svc 1  │ svc 2  │ svc N  │
# └────────┴────────┴────────┘
# Args: $1=workdir $2=session $3=branch
# Requires: python3 (for JSON parsing), tnt worktree run

set -e

WORKDIR="$1"
SESSION="$2"
BRANCH="$3"
TNT_DIR="${HOME}/.config/tnt"

[[ -z "$WORKDIR" || -z "$SESSION" || -z "$BRANCH" ]] && {
  echo "Usage: run.sh <workdir> <session> <branch>"; exit 1
}

existing=$(tmux list-windows -t "$SESSION" -F '#{window_id} #{@run}' 2>/dev/null | grep ' 1$' | head -1 | cut -d' ' -f1)
if [[ -n "$existing" ]]; then
  tnt worktree run switch "$WORKDIR"
  exit 0
fi

REPO_NAME="$SESSION"
CONFIG_FILE="$TNT_DIR/projects/$REPO_NAME/config.json"

if [[ ! -f "$CONFIG_FILE" ]]; then
  read -p "No config for $REPO_NAME. Create one? [y/N] " answer
  [[ "$answer" != [yY] ]] && exit 0
  mkdir -p "$(dirname "$CONFIG_FILE")"
  cat > "$CONFIG_FILE" << 'TEMPLATE'
{
  "default_layout": "dev",
  "services": [
    { "name": "app", "run": "echo 'replace with your command'", "cwd": "." }
  ]
}
TEMPLATE
  ${EDITOR:-vi} "$CONFIG_FILE"
  exit 0
fi

env_cmd=$(python3 -c "
import json
with open('$CONFIG_FILE') as f:
    print(json.load(f).get('env', ''))
" 2>/dev/null || true)

services=$(python3 -c "
import json
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
for svc in cfg.get('services', []):
    print(svc.get('name', 'unnamed') + '\t' + svc.get('run', '') + '\t' + svc.get('cwd', '.'))
" 2>/dev/null || true)

if [[ -z "$services" ]]; then
  tmux display-message "No services in $CONFIG_FILE"
  exit 1
fi

first_idx=$(tmux list-windows -t "$SESSION" -F '#{window_index}' | sort -n | head -1)
WID=$(tmux new-window -P -F '#{window_id}' -t "$SESSION" -n "${BRANCH##*/}:run" -c "$WORKDIR")
tmux set-option -w -t "$WID" @worktree "$BRANCH"
tmux set-option -w -t "$WID" @run "1"
tmux swap-window -s "$WID" -t "$SESSION:$first_idx" 2>/dev/null || true

idx=0
while IFS=$'\t' read -r name cmd cwd; do
  [[ -z "$cmd" ]] && continue
  svc_dir="$WORKDIR/$cwd"
  if [[ $idx -eq 0 ]]; then
    tmux send-keys -t "$WID" "cd '$svc_dir'" Enter
    [[ -n "$env_cmd" ]] && tmux send-keys -t "$WID" "$env_cmd" Enter
    tmux send-keys -t "$WID" "$cmd" Enter
  else
    pane_id=$(tmux split-window -t "$WID" -h -c "$svc_dir" -P -F '#{pane_id}')
    sleep 0.3
    [[ -n "$env_cmd" ]] && tmux send-keys -t "$pane_id" "$env_cmd" Enter
    tmux send-keys -t "$pane_id" "$cmd" Enter
  fi
  idx=$((idx + 1))
done <<< "$services"

tmux select-layout -t "$WID" even-horizontal 2>/dev/null || true
tmux select-pane -t "$WID.1"
tmux display-message "Started $idx service(s)"
