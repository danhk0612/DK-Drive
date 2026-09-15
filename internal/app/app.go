package app

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const Version = "0.9.1"
const Creator = "참빛바다"

func Run(args []string) error {
	return run(args, os.Stdout)
}

func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprintf(output, "DK-Drive %s\n정식 배포 전 검증 단계입니다. GUI는 Windows에서 실행하세요.\n", Version)
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

const WinFspNotice = "WinFsp - Windows File System Proxy, Copyright (C) Bill Zissimopoulos"
const WinFspURL = "https://github.com/winfsp/winfsp"
