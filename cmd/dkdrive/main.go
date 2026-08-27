package main

import (
	"fmt"
	"os"

	"github.com/danhk0612/DK-Drive/internal/app"
)

func main() {
	var err error
	desktop := len(os.Args) == 1 || len(os.Args) == 2 && (os.Args[1] == "gui" || os.Args[1] == "--tray")
	if desktop {
		err = runDesktop(len(os.Args) == 2 && os.Args[1] == "--tray")
	} else {
		err = app.Run(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dkdrive:", err)
		if desktop {
			showStartupError(err)
		}
		os.Exit(1)
	}
}
