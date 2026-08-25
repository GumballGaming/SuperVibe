//go:build !windows

package procx

import "syscall"

func hideWindow() *syscall.SysProcAttr {
	return nil
}
