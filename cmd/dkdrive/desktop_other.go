//go:build !windows

package main

import "github.com/danhk0612/DK-Drive/internal/app"

func runDesktop(bool) error  { return app.Run(nil) }
func showStartupError(error) {}
