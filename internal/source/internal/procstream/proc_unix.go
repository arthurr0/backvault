//go:build unix

package procstream

import (
	"os/exec"
	"syscall"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func terminate(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid <= 1 {
		return cmd.Process.Kill()
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return cmd.Process.Kill()
	}
	time.AfterFunc(killGrace, func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	return nil
}
