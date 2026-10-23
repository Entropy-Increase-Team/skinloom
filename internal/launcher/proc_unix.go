//go:build !windows

package launcher

import (
	"log/slog"
	"os/exec"
	"syscall"
)

// prepare starts the child in its own process group so we can signal the whole tree.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeathSignal(cmd.SysProcAttr)
}

// adopt is a no-op here: Linux uses the parent-death signal set in prepare; on macOS
// an orphaned frontend is stopped by the next `skinloom up`, which picks a free port.
func adopt(*exec.Cmd, *slog.Logger) {}

// terminate asks the process group to shut down gracefully.
func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}
