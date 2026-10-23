//go:build windows

package launcher

import (
	"log/slog"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// prepare puts the child in its own process group so console Ctrl+C reaches only us.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

var (
	jobOnce sync.Once
	job     windows.Handle
	jobErr  error
)

// adopt puts the child (and every process it starts later) into a job object that is
// killed when skinloom's handle to it closes — including when skinloom crashes or is
// ended from Task Manager. Without it an orphaned Node kept the frontend port busy.
func adopt(cmd *exec.Cmd, log *slog.Logger) {
	jobOnce.Do(func() {
		job, jobErr = windows.CreateJobObject(nil, nil)
		if jobErr != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		_, jobErr = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	})
	if jobErr != nil {
		log.Warn("frontend will not be stopped automatically if skinloom crashes", "err", jobErr)
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		log.Warn("attach frontend to job object", "err", err)
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		log.Warn("attach frontend to job object", "err", err)
	}
}

// terminate kills the whole process tree: pnpm.cmd → node children would otherwise survive.
func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := kill.Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
