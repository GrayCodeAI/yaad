//go:build windows

package daemon

import "os/exec"

func setSysProcAttr(_ *exec.Cmd) {}
