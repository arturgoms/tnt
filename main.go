package main

import (
	"embed"

	"github.com/arturgoms/tnt/cmd"
)

//go:embed config.example.toml
var exampleConfig []byte

//go:embed layouts
var layoutsFS embed.FS

//go:embed scripts
var scriptsFS embed.FS

//go:embed projects/example/config.json
var projectConfigExample []byte

func main() {
	cmd.ExampleConfig = exampleConfig
	cmd.LayoutsFS = layoutsFS
	cmd.ScriptsFS = scriptsFS
	cmd.ProjectConfigExample = projectConfigExample
	cmd.Execute()
}
