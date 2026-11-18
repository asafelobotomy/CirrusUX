//go:build !unix

package gateway

func umask(int) int { return 0 }
