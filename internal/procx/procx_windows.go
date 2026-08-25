package procx

import "syscall"

func hideWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

func SysProcAttr() *syscall.SysProcAttr {
	return hideWindow()
}
