package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Quit   key.Binding
	Help   key.Binding
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Back   key.Binding
	Filter key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j", "down")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	}
}
