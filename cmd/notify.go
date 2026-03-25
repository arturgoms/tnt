package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func notifySend(stateDir, message, color string, ttl int) {
	dir := filepath.Join(stateDir, "notifications")
	os.MkdirAll(dir, 0755)

	now := time.Now().Unix()
	expiry := now + int64(ttl)
	id := fmt.Sprintf("%d", time.Now().UnixNano())

	content := fmt.Sprintf("%s\n%s\n%d\n", message, color, expiry)
	os.WriteFile(filepath.Join(dir, id), []byte(content), 0644)
}

func notifyClear(stateDir string) {
	dir := filepath.Join(stateDir, "notifications")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}
}

func notifyRead(stateDir string) string {
	dir := filepath.Join(stateDir, "notifications")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	now := time.Now().Unix()
	var parts []string

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		lines := strings.SplitN(string(data), "\n", 4)
		if len(lines) < 3 {
			continue
		}

		msg := lines[0]
		clr := lines[1]
		exp, err := strconv.ParseInt(strings.TrimSpace(lines[2]), 10, 64)
		if err != nil {
			continue
		}

		if now > exp {
			os.Remove(path)
			continue
		}

		parts = append(parts, fmt.Sprintf("#[fg=%s,bold] %s #[default]", clr, msg))
	}

	return strings.Join(parts, "")
}

func runNotify(args []string) {
	cfg := app.Config

	color := "#E6B450"
	ttl := 30
	message := ""
	mode := "send"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--clear":
			mode = "clear"
		case "--read":
			mode = "read"
		case "--color":
			if i+1 < len(args) {
				color = args[i+1]
				i++
			}
		case "--ttl":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil {
					ttl = v
				}
				i++
			}
		default:
			message = args[i]
		}
	}

	switch mode {
	case "send":
		if message == "" {
			fmt.Fprintln(os.Stderr, "usage: tnt notify \"message\" [--color COLOR] [--ttl SECONDS]")
			os.Exit(1)
		}
		notifySend(cfg.Paths.State, message, color, ttl)
	case "clear":
		notifyClear(cfg.Paths.State)
	case "read":
		output := notifyRead(cfg.Paths.State)
		if output != "" {
			fmt.Print(output)
		}
	}
}
