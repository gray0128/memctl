package main

import (
	"os"

	"github.com/gray0128/memctl/internal/cli"
)

var version = "v2.0.0-dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}
