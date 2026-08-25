package main

import (
	"os"

	_ "time/tzdata" // embed tz database for Europe/Berlin

	"mailbox/src/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
