package app

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const Version = "0.2.0-dev"

func Run(args []string) error {
	return run(args, os.Stdout)
}

func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "DKDrive %s\n0.2 SFTP 기본 기능 개발 단계입니다.\n", Version)
		return err
	}

	switch args[0] {
	case "version", "--version", "-v":
		_, err := fmt.Fprintln(output, Version)
		return err
	default:
		return errors.New("지원하지 않는 명령입니다")
	}
}
