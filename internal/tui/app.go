package tui

import (
	"github.com/arturgoms/tnt/internal/config"
	"github.com/arturgoms/tnt/internal/theme"
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
