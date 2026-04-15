package main

import (
	_ "embed"

	"github.com/arturgoms/tnt/cmd"
)

//go:embed config.example.toml
var exampleConfig []byte

func main() {
	cmd.ExampleConfig = exampleConfig
	cmd.Execute()
}
