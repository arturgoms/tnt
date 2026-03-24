package tui

import (
	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/theme"
)

type App struct {
	Config *config.Config
	Theme  *theme.Theme
}

func NewApp(cfg *config.Config) *App {
	return &App{
		Config: cfg,
		Theme:  theme.New(cfg.Theme),
	}
}
