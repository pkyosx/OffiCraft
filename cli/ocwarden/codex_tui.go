package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	codexSessionLogName      = "codex-session.log"
	codexAppServerStartLimit = 30 * time.Second
	codexAppServerStopGrace  = 5 * time.Second
	codexTUIRetryMin         = time.Second
	codexTUIRetryMax         = 30 * time.Second
	// A TUI that lived at least this long was a real attach, not a failed one,
	// so the next reopen starts again from the shortest delay.
	codexTUIStableRun = 10 * time.Second
)

// The pane's terminal. Swappable so a test's stub TUI never reads the test
// binary's own stdin.
var (
	codexTUIStdin  io.Reader = os.Stdin
	codexTUIStdout io.Writer = os.Stdout
)

// The previous generation survives one restart so the log of a session that
// just died is still there to read.
func openCodexSessionLog(path string) (*os.File, error) {
	_ = os.Rename(path, path+".1")
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
}

func waitForCodexAppServer(socketPath string, exited <-chan struct{}) (*codexAppConn, error) {
	deadline := time.Now().Add(codexAppServerStartLimit)
	for {
		select {
		case <-exited:
			return nil, errors.New("app-server exited before listening")
		default:
		}
		conn, err := dialCodexAppServer(socketPath, 2*time.Second)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("app-server not listening after %s: %v", codexAppServerStartLimit, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The App Server runs in its own process group (out of the terminal's reach),
// so it and the commands it spawned are signalled as that group. SIGTERM lets
// it remove its socket; the forced path removes it here instead.
func stopCodexAppServer(pid int, exited <-chan struct{}, socketPath string) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(codexAppServerStopGrace):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
	removeCodexAppServerSocket(socketPath)
}

func codexTUICommand(ctx context.Context, codexBin, workdir, threadID, socketPath string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, codexBin, "resume", threadID,
		"--remote", "unix://"+socketPath, "-c", "check_for_update_on_startup=false")
	cmd.Dir = workdir
	cmd.Stdin = codexTUIStdin
	cmd.Stdout = codexTUIStdout
	cmd.Stderr = codexTUIStdout
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	return cmd
}

// Closing the TUI must never take the member offline: the member's work lives
// in the App Server, so the TUI is only reopened, with a growing delay while it
// keeps failing straight away.
func runCodexTUI(ctx context.Context, newCmd func(context.Context) *exec.Cmd, term io.Writer,
	sleep func(context.Context, time.Duration) bool) {
	delay := codexTUIRetryMin
	for {
		started := time.Now()
		err := newCmd(ctx).Run()
		if ctx.Err() != nil {
			return
		}
		ran := time.Since(started)
		if ran >= codexTUIStableRun {
			delay = codexTUIRetryMin
		}
		reason := "已結束"
		if err != nil {
			reason = err.Error()
		}
		fmt.Fprintf(term, "\r\nOffiCraft：Codex 畫面已關閉（%s），%s 後重新接上。成員在背景照常工作。\r\n", reason, delay)
		if !sleep(ctx, delay) {
			return
		}
		if ran < codexTUIStableRun {
			delay = min(delay*2, codexTUIRetryMax)
		}
	}
}
