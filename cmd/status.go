package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/agents"
)

func targetToFile(target string) string {
	r := strings.NewReplacer(":", "-", ".", "-")
	return r.Replace(target)
}

func extractWinIdx(target string) string {
	after := target[strings.Index(target, ":")+1:]
	if dot := strings.Index(after, "."); dot >= 0 {
		return after[:dot]
	}
	return after
}

func readStateFile(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func runStatus() {
	cfg := app.Config
	stateDir := cfg.Paths.State

	currentSession := ""
	if out, err := exec.Command("tmux", "display-message", "-p", "#S").Output(); err == nil {
		currentSession = strings.TrimSpace(string(out))
	}

	now := time.Now().Unix()

	if !cfg.Integrations.Opencode {
		if n := notifyRead(stateDir); n != "" {
			fmt.Printf(" %s ", n)
		}
		return
	}

	detected := agents.Detect("")

	agentWindows := map[string]bool{}
	var otherWaiting []string
	var otherRunning []string
	otherWaitingSeen := map[string]bool{}
	otherRunningSeen := map[string]bool{}

	for _, a := range detected {
		winIdx := extractWinIdx(a.Target)
		winKey := a.Session + ":" + winIdx
		agentWindows[winKey] = true

		effectiveState := string(a.Status)
		stateFile := filepath.Join(stateDir, targetToFile(a.Target))
		prevStateFile := filepath.Join(stateDir, "prev_"+targetToFile(a.Target))

		if a.Status == agents.StatusIdle {
			lastActive := readStateFile(stateFile)
			if lastActive > 0 && (now-lastActive) < 180 {
				effectiveState = "waiting"
			}
		}
		if a.Status == agents.StatusRunning || a.Status == agents.StatusWaiting {
			os.WriteFile(stateFile, []byte(fmt.Sprintf("%d", now)), 0644)
		}

		prevState := ""
		if data, err := os.ReadFile(prevStateFile); err == nil {
			prevState = strings.TrimSpace(string(data))
		}
		if prevState == "running" && effectiveState != "running" {
			if a.Status == agents.StatusWaiting {
				notifySend(stateDir, fmt.Sprintf("● %s/%s needs input", a.Session, a.WindowName), "#E6B450", 60)
			} else {
				notifySend(stateDir, fmt.Sprintf("● %s/%s done", a.Session, a.WindowName), "#AAD94C", 30)
			}
		}
		os.WriteFile(prevStateFile, []byte(effectiveState), 0644)

		exec.Command("tmux", "set-option", "-w", "-t", winKey, "@agent_state", effectiveState).Run()

		if a.Session == currentSession {
			// tracked for future use
		} else {
			switch a.Status {
			case agents.StatusWaiting:
				if !otherWaitingSeen[a.Session] {
					otherWaitingSeen[a.Session] = true
					otherWaiting = append(otherWaiting, a.Session)
				}
			case agents.StatusRunning:
				if !otherRunningSeen[a.Session] {
					otherRunningSeen[a.Session] = true
					otherRunning = append(otherRunning, a.Session)
				}
			}
		}
	}

	cleanStaleAgentState(agentWindows)

	notifications := notifyRead(stateDir)
	segment := ""
	if notifications != "" {
		segment += notifications
	}

	if len(otherWaiting) > 0 {
		names := strings.Join(otherWaiting, ",")
		segment += fmt.Sprintf(" #[fg=#E6B450]● %s#[default]", names)
	}

	if segment == "" {
		fmt.Print("")
		return
	}

	fmt.Printf(" %s ", segment)
}

func cleanStaleAgentState(agentWindows map[string]bool) {
	out, err := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index}").Output()
	if err != nil {
		return
	}
	for _, winKey := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if winKey == "" {
			continue
		}
		if !agentWindows[winKey] {
			exec.Command("tmux", "set-option", "-wu", "-t", winKey, "@agent_state").Run()
		}
	}
}
