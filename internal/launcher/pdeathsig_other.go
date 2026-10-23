//go:build !linux && !windows

package launcher

import "syscall"

func setParentDeathSignal(*syscall.SysProcAttr) {}
