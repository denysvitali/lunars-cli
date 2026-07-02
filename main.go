package main

import (
	"fmt"
	"os"

	"lunars-cli/internal/cli"
)

var version = "0.1.0"

func main() {
	root := cli.NewRootCommand(os.Stdout, os.Stderr, version)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "lunars: %v\n", err)
		os.Exit(1)
	}
}
