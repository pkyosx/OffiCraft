package main

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCodexTUI(t *testing.T) {
	notice := func(reason, delay string) string {
		return "\r\nOffiCraft：Codex 畫面已關閉（" + reason + "），" + delay + " 後重新接上。成員在背景照常工作。\r\n"
	}

	t.Run("a TUI that keeps exiting straight away is reopened with a delay doubling up to 30s", func(t *testing.T) {
		ctx := context.Background()
		launches := 0
		var delays []time.Duration
		term := &lockedBuffer{}

		runCodexTUI(ctx, func(ctx context.Context) *exec.Cmd {
			launches++
			return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
		}, term, func(_ context.Context, d time.Duration) bool {
			delays = append(delays, d)
			return len(delays) < 7
		})

		wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
			16 * time.Second, 30 * time.Second, 30 * time.Second}
		if !reflect.DeepEqual(delays, wantDelays) {
			t.Errorf("delays = %v, want %v", delays, wantDelays)
		}
		if launches != 7 {
			t.Errorf("launches = %d, want 7", launches)
		}
		want := notice("已結束", "1s") + notice("已結束", "2s") + notice("已結束", "4s") + notice("已結束", "8s") +
			notice("已結束", "16s") + notice("已結束", "30s") + notice("已結束", "30s")
		if term.String() != want {
			t.Errorf("terminal =\n%q\nwant\n%q", term.String(), want)
		}
	})

	t.Run("a TUI that fails names its exit status on the terminal", func(t *testing.T) {
		term := &lockedBuffer{}

		runCodexTUI(context.Background(), func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 3")
		}, term, func(context.Context, time.Duration) bool { return false })

		if want := notice("exit status 3", "1s"); term.String() != want {
			t.Errorf("terminal = %q, want %q", term.String(), want)
		}
	})

	t.Run("a TUI that stayed attached for 10s resets the delay to 1s", func(t *testing.T) {
		launches := 0
		var delays []time.Duration

		runCodexTUI(context.Background(), func(ctx context.Context) *exec.Cmd {
			launches++
			if launches == 3 {
				time.Sleep(codexTUIStableRun)
			}
			return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
		}, &lockedBuffer{}, func(_ context.Context, d time.Duration) bool {
			delays = append(delays, d)
			return len(delays) < 3
		})

		if want := []time.Duration{time.Second, 2 * time.Second, time.Second}; !reflect.DeepEqual(delays, want) {
			t.Errorf("delays = %v, want %v", delays, want)
		}
	})

	t.Run("a cancelled session does not reopen the TUI or print anything", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		launches, sleeps := 0, 0
		term := &lockedBuffer{}

		runCodexTUI(ctx, func(ctx context.Context) *exec.Cmd {
			launches++
			cancel()
			return exec.CommandContext(context.Background(), "/bin/sh", "-c", "exit 0")
		}, term, func(context.Context, time.Duration) bool { sleeps++; return true })

		if launches != 1 || sleeps != 0 || term.String() != "" {
			t.Errorf("launches=%d sleeps=%d terminal=%q, want 1, 0, empty", launches, sleeps, term.String())
		}
	})
}

func TestCodexTUICommand(t *testing.T) {
	t.Run("the TUI resumes the member's thread on its own App Server socket", func(t *testing.T) {
		screen := silenceCodexTUI(t)

		cmd := codexTUICommand(context.Background(), "/opt/codex", "/w/m1", "th_1", "/w/m1/.codex-app.sock")

		want := []string{"/opt/codex", "resume", "th_1", "--remote", "unix:///w/m1/.codex-app.sock",
			"-c", "check_for_update_on_startup=false"}
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Errorf("args = %q, want %q", cmd.Args, want)
		}
		if cmd.Dir != "/w/m1" {
			t.Errorf("dir = %q, want /w/m1", cmd.Dir)
		}
		if cmd.Stdin != codexTUIStdin || cmd.Stdout != screen || cmd.Stderr != screen {
			t.Error("the TUI is not wired to the pane's terminal")
		}
	})

	t.Run("ending the session sends the TUI SIGTERM", func(t *testing.T) {
		silenceCodexTUI(t)
		dir := t.TempDir()
		got, ready := filepath.Join(dir, "signal"), filepath.Join(dir, "ready")
		stub := filepath.Join(dir, "codex")
		script := "#!/bin/sh\ntrap 'echo TERM > " + shellQuote(got) + "; exit 0' TERM\ntouch " + shellQuote(ready) +
			"\nwhile :; do sleep 0.1; done\n"
		if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cmd := codexTUICommand(ctx, stub, dir, "th_1", "/w/s.sock")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("stub TUI never started")
			}
		}

		cancel()
		_ = cmd.Wait()

		if raw, err := os.ReadFile(got); err != nil || string(raw) != "TERM\n" {
			t.Errorf("the TUI recorded %q, %v; want TERM", raw, err)
		}
	})
}

func TestOpenCodexSessionLog(t *testing.T) {
	t.Run("the previous log is kept as .1 and the new log starts empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), codexSessionLogName)
		if err := os.WriteFile(path, []byte("last session\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		f, err := openCodexSessionLog(path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("this session\n")
		_ = f.Close()

		if raw, _ := os.ReadFile(path + ".1"); string(raw) != "last session\n" {
			t.Errorf("%s.1 = %q, want the previous log", path, raw)
		}
		if raw, _ := os.ReadFile(path); string(raw) != "this session\n" {
			t.Errorf("%s = %q, want only this session's lines", path, raw)
		}
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("log mode = %v, want 0600", info.Mode().Perm())
		}
	})

	t.Run("a first session creates the log without a .1", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), codexSessionLogName)

		f, err := openCodexSessionLog(path)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()

		if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
			t.Errorf("a .1 exists without a previous log: %v", err)
		}
	})
}

func TestWaitForCodexAppServer(t *testing.T) {
	t.Run("an App Server that exits before listening is reported", func(t *testing.T) {
		exited := make(chan struct{})
		close(exited)

		_, err := waitForCodexAppServer(filepath.Join(shortTempDir(t), "s.sock"), exited)

		if err == nil || err.Error() != "app-server exited before listening" {
			t.Errorf("error = %v, want app-server exited before listening", err)
		}
	})

	t.Run("an App Server that comes up late is connected once it listens", func(t *testing.T) {
		path := filepath.Join(shortTempDir(t), "s.sock")
		go func() {
			time.Sleep(300 * time.Millisecond)
			ln, err := net.Listen("unix", path)
			if err != nil {
				return
			}
			defer ln.Close()
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			req, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte(upgradeAnswer(req.Header.Get("Sec-WebSocket-Key"))))
			time.Sleep(200 * time.Millisecond)
		}()

		conn, err := waitForCodexAppServer(path, make(chan struct{}))

		if err != nil {
			t.Fatalf("waitForCodexAppServer: %v", err)
		}
		_ = conn.Close()
	})
}

func TestStopCodexAppServer(t *testing.T) {
	start := func(t *testing.T, script string) (int, int, chan struct{}) {
		t.Helper()
		dir := t.TempDir()
		childFile := filepath.Join(dir, "child")
		cmd := exec.Command("/bin/sh", "-c", strings.ReplaceAll(script, "CHILD", shellQuote(childFile)))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(exited) }()
		var child int
		for deadline := time.Now().Add(5 * time.Second); child == 0; time.Sleep(20 * time.Millisecond) {
			raw, _ := os.ReadFile(childFile)
			child, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			if time.Now().After(deadline) {
				t.Fatal("the fake App Server never started its child")
			}
		}
		return cmd.Process.Pid, child, exited
	}
	gone := func(pid int) bool {
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if syscall.Kill(pid, 0) != nil {
				return true
			}
		}
		return false
	}
	socketAt := func(t *testing.T) string {
		path := filepath.Join(shortTempDir(t), ".codex-app.sock")
		leaveUnixSocket(t, path)
		return path
	}

	t.Run("SIGTERM reaches the App Server and the commands it spawned, and the socket is removed", func(t *testing.T) {
		pid, child, exited := start(t, "sleep 30 & echo $! > CHILD; wait")
		socket := socketAt(t)

		stopCodexAppServer(pid, exited, socket)

		if !gone(child) {
			t.Errorf("the App Server's child %d survived the stop", child)
		}
		if _, err := os.Lstat(socket); !os.IsNotExist(err) {
			t.Errorf("the socket survived the stop: %v", err)
		}
	})

	t.Run("an App Server ignoring SIGTERM is killed after the grace period and the socket is removed", func(t *testing.T) {
		pid, child, exited := start(t, "trap '' TERM; echo $$ > CHILD; while :; do sleep 0.1; done")
		socket := socketAt(t)

		began := time.Now()
		stopCodexAppServer(pid, exited, socket)

		if took := time.Since(began); took < codexAppServerStopGrace {
			t.Errorf("the stop returned after %v, want it to wait the %v grace first", took, codexAppServerStopGrace)
		}
		if !gone(child) {
			t.Errorf("the App Server %d survived the forced stop", child)
		}
		if _, err := os.Lstat(socket); !os.IsNotExist(err) {
			t.Errorf("the socket survived the forced stop: %v", err)
		}
	})
}
