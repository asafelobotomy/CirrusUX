//go:build !linux

package gateway

import (
	"errors"
	"os"
	"os/exec"
)

var errNoPTY = errors.New("the terminal needs a Linux gateway")

func startPTY(*exec.Cmd, int, int) (*os.File, error) { return nil, errNoPTY }

func setPTYSize(*os.File, int, int) error { return errNoPTY }
