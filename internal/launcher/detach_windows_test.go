//go:build windows

package launcher

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in its own process group. It cannot leave the launcher's job
// object (breakaway is not allowed — that is the point of the job), but it keeps the
// inherited pipes open after its parent exits, which is what used to hang the supervisor.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
