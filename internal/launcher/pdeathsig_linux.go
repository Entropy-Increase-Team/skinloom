//go:build linux

package launcher

import "syscall"

// setParentDeathSignal makes the kernel send SIGTERM to the frontend if skinloom dies
// without a chance to stop it (SIGKILL, OOM killer).
func setParentDeathSignal(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGTERM }
