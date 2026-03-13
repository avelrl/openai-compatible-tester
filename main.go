package main

import (
	"os"

	"github.com/avelrl/openai-compatible-tester/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
