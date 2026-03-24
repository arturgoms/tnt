package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

func Run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func SendKeys(target, keys string) error {
	_, err := Run("send-keys", "-t", target, keys, "Enter")
	return err
}

func DisplayMessage(target, msg string) error {
	_, err := Run("display-message", "-t", target, msg)
	return err
}

func ListPanes(session string, format string) ([]string, error) {
	args := []string{"list-panes", "-a", "-F", format}
	if session != "" {
		args = []string{"list-panes", "-t", session, "-F", format}
	}
	out, err := Run(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func ListWindows(session, format string) ([]string, error) {
	out, err := Run("list-windows", "-t", session, "-F", format)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func NewWindow(session, name, workdir string) (string, error) {
	return Run("new-window", "-P", "-F", "#{window_id}", "-t", session, "-n", name, "-c", workdir)
}

func SelectWindow(target string) error {
	_, err := Run("select-window", "-t", target)
	return err
}

func SplitWindow(target string, horizontal bool, size string, workdir string) (string, error) {
	args := []string{"split-window", "-t", target, "-P", "-F", "#{pane_id}", "-c", workdir}
	if horizontal {
		args = append(args, "-h")
	} else {
		args = append(args, "-v")
	}
	if size != "" {
		args = append(args, "-l", size)
	}
	return Run(args...)
}

func SetWindowOption(target, key, value string) error {
	_, err := Run("set-option", "-w", "-t", target, key, value)
	return err
}

func GetWindowOption(target, key string) (string, error) {
	return Run("show-option", "-wv", "-t", target, key)
}

func SessionName() (string, error) {
	return Run("display-message", "-p", "#S")
}

func CurrentPane() (string, error) {
	return Run("display-message", "-p", "#{pane_id}")
}

func Notify(msg string, ttl int, color string) error {
	scripts, err := Run("display-message", "-p", "#{session_path}")
	if err != nil {
		return err
	}
	_ = scripts
	cmd := exec.Command("tnt-notify", msg, "--ttl", fmt.Sprintf("%d", ttl), "--color", color)
	return cmd.Run()
}

func HasSession(name string) bool {
	err := exec.Command("tmux", "has-session", "-t="+name).Run()
	return err == nil
}

func InTmux() bool {
	return exec.Command("tmux", "display-message", "-p", "").Run() == nil
}
