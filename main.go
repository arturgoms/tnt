package main

import (
	"embed"

	"github.com/arturgoms/tnt/cmd"
)

//go:embed config.example.toml
var exampleConfig []byte

//go:embed layouts
var layoutsFS embed.FS

func main() {
	cmd.ExampleConfig = exampleConfig
	cmd.LayoutsFS = layoutsFS
	cmd.Execute()
}
