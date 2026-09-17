//go:build !unix

package procstream

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}

func terminate(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
