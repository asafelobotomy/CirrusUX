//go:build unix

package gateway

import "syscall"

func umask(m int) int { return syscall.Umask(m) }
