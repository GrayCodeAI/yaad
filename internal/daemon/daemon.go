// Package daemon manages yaad's background server lifecycle:
// PID file tracking, health checks, and auto-start logic.
package daemon

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultAddr    = ":3456"
	healthPath     = "/yaad/health"
	pidFileName    = "yaad.pid"
	startTimeout   = 5 * time.Second
	healthTimeout  = 2 * time.Second
)

// PIDFile returns the path to the PID file for the given project directory.
func PIDFile(projectDir string) string {
	return filepath.Join(projectDir, ".yaad", pidFileName)
}

// WritePID writes the current process PID to the PID file.
func WritePID(projectDir string) error {
	dir := filepath.Join(projectDir, ".yaad")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(PIDFile(projectDir), []byte(strconv.Itoa(os.Getpid())), 0644)
}

// RemovePID removes the PID file.
func RemovePID(projectDir string) {
	os.Remove(PIDFile(projectDir))
}

// ReadPID reads the stored PID. Returns 0 if no PID file or invalid.
func ReadPID(projectDir string) int {
	b, err := os.ReadFile(PIDFile(projectDir))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}

// IsRunning checks if the daemon process is alive (signal 0 check).
func IsRunning(projectDir string) bool {
	pid := ReadPID(projectDir)
	if pid == 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0: check if process exists without sending a real signal
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// HealthCheck pings the yaad health endpoint. Returns nil if healthy.
// addr can be ":port", "host:port", or a full "http://host:port" URL.
func HealthCheck(addr string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	var url string
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		url = addr + healthPath
	} else if strings.HasPrefix(addr, ":") {
		url = "http://localhost" + addr + healthPath
	} else {
		url = "http://" + addr + healthPath
	}
	client := &http.Client{Timeout: healthTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("yaad not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("yaad unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

// Stop sends SIGTERM to the daemon process.
func Stop(projectDir string) error {
	pid := ReadPID(projectDir)
	if pid == 0 {
		return fmt.Errorf("no yaad daemon found (no PID file)")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		RemovePID(projectDir)
		return fmt.Errorf("could not signal process %d: %w", pid, err)
	}
	// Wait briefly for shutdown
	for i := 0; i < 10; i++ {
		time.Sleep(200 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break
		}
	}
	RemovePID(projectDir)
	return nil
}

// EnsureRunning checks health; if yaad is not running, launches it as a daemon.
// Returns the addr the server is (or will be) listening on.
func EnsureRunning(projectDir, addr string) error {
	if addr == "" {
		addr = DefaultAddr
	}

	// Fast path: already healthy
	if HealthCheck(addr) == nil {
		return nil
	}

	// Process exists but unhealthy — stale PID
	if IsRunning(projectDir) {
		Stop(projectDir)
	}

	// Launch daemon (without --daemon flag so it runs in foreground in the child)
	exe, err := os.Executable()
	if err != nil {
		exe = "yaad"
	}
	cmd := exec.Command(exe, "serve", "--addr", addr)
	cmd.Dir = projectDir
	cmd.Stdout = nil
	cmd.Stderr = nil
	setSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start yaad daemon: %w", err)
	}
	// Detach — don't wait for child
	cmd.Process.Release()

	// Poll until healthy or timeout
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if HealthCheck(addr) == nil {
			return nil
		}
	}
	return fmt.Errorf("yaad daemon started but not healthy after %s", startTimeout)
}

func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return ":" + addr
}
