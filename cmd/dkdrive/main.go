package main

import (
	"fmt"
	"os"

	"github.com/danhk0612/DK-Drive/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dkdrive:", err)
		os.Exit(1)
	}
}
