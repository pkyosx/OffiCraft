package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
)

type startCall struct {
	path string
	argv []string
}

type fakeChild struct {
	forwarded []os.Signal
	ack       chan os.Signal
	wait      func() (*os.ProcessState, error)
}

func (c *fakeChild) Signal(s os.Signal) error {
	c.forwarded = append(c.forwarded, s)
	c.ack <- s
	return nil
}

func (c *fakeChild) Wait() (*os.ProcessState, error) { return c.wait() }

func stateOf(t *testing.T, script string) *os.ProcessState {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	_ = cmd.Run()
	if cmd.ProcessState == nil {
		t.Fatalf("%q left no process state", script)
	}
	return cmd.ProcessState
}

func TestRealMain(t *testing.T) {
	origExecutable, origStart, origNotify, origStop := executable, startChild, notify, stop
	defer func() {
		executable, startChild, notify, stop = origExecutable, origStart, origNotify, origStop
	}()

	exited7 := stateOf(t, "exit 7")

	cases := []struct {
		name          string
		args          []string
		exe           string
		exeErr        error
		startErr      error
		wait          func(signals chan<- os.Signal, c *fakeChild) (*os.ProcessState, error)
		wantCode      int
		wantOut       string
		wantStarted   []startCall
		wantNotified  []os.Signal
		wantStopped   int
		wantForwarded []os.Signal
	}{
		{
			name:     "any argument prints usage and starts nothing",
			args:     []string{"run"},
			exe:      "/opt/officraft/bin/officraft",
			wantCode: 2,
			wantOut:  "usage: officraft\n",
		},
		{
			name:     "an unresolvable own path starts nothing",
			exeErr:   errors.New("no such file or directory"),
			wantCode: 1,
			wantOut:  "[officraft] FATAL: cannot resolve own path: no such file or directory\n",
		},
		{
			name:     "a child that will not start leaves no signal handler installed",
			exe:      "/opt/officraft/bin/officraft",
			startErr: errors.New("permission denied"),
			wantCode: 1,
			wantOut:  "[officraft] FATAL: cannot start sibling ocwarden: permission denied\n",
			wantStarted: []startCall{{
				path: "/opt/officraft/bin/ocwarden",
				argv: []string{"/opt/officraft/bin/ocwarden", "run"},
			}},
		},
		{
			name: "a failed wait is fatal and still uninstalls the handler",
			exe:  "/opt/officraft/bin/officraft",
			wait: func(chan<- os.Signal, *fakeChild) (*os.ProcessState, error) {
				return nil, errors.New("no child processes")
			},
			wantCode: 1,
			wantOut:  "[officraft] FATAL: wait for ocwarden: no child processes\n",
			wantStarted: []startCall{{
				path: "/opt/officraft/bin/ocwarden",
				argv: []string{"/opt/officraft/bin/ocwarden", "run"},
			}},
			wantNotified: []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP},
			wantStopped:  1,
		},
		{
			name: "every forwarded signal reaches the child and its exit status is returned",
			exe:  "/opt/officraft/bin/officraft",
			wait: func(signals chan<- os.Signal, c *fakeChild) (*os.ProcessState, error) {
				for _, s := range []os.Signal{syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM} {
					signals <- s
					<-c.ack
				}
				return exited7, nil
			},
			wantCode: 7,
			wantStarted: []startCall{{
				path: "/opt/officraft/bin/ocwarden",
				argv: []string{"/opt/officraft/bin/ocwarden", "run"},
			}},
			wantNotified:  []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP},
			wantStopped:   1,
			wantForwarded: []os.Signal{syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var started []startCall
			var notified []os.Signal
			var notifiedChan chan<- os.Signal
			stopped := 0
			kid := &fakeChild{ack: make(chan os.Signal)}
			kid.wait = func() (*os.ProcessState, error) {
				if c.wait == nil {
					t.Fatalf("wait was called but the case defines none")
				}
				return c.wait(notifiedChan, kid)
			}

			executable = func() (string, error) { return c.exe, c.exeErr }
			startChild = func(path string, argv []string) (child, error) {
				started = append(started, startCall{path: path, argv: argv})
				if c.startErr != nil {
					return nil, c.startErr
				}
				return kid, nil
			}
			notify = func(ch chan<- os.Signal, sigs ...os.Signal) {
				notifiedChan = ch
				notified = append(notified, sigs...)
			}
			stop = func(chan<- os.Signal) { stopped++ }

			out := &bytes.Buffer{}
			code := realMain(c.args, out)

			if code != c.wantCode {
				t.Errorf("exit code = %d, want %d", code, c.wantCode)
			}
			if out.String() != c.wantOut {
				t.Errorf("output = %q, want %q", out.String(), c.wantOut)
			}
			if !reflect.DeepEqual(started, c.wantStarted) {
				t.Errorf("started = %+v, want %+v", started, c.wantStarted)
			}
			if !reflect.DeepEqual(notified, c.wantNotified) {
				t.Errorf("notified = %v, want %v", notified, c.wantNotified)
			}
			if stopped != c.wantStopped {
				t.Errorf("stop calls = %d, want %d", stopped, c.wantStopped)
			}
			if !reflect.DeepEqual(kid.forwarded, c.wantForwarded) {
				t.Errorf("signals delivered to the child = %v, want %v", kid.forwarded, c.wantForwarded)
			}
		})
	}
}

func TestExitStatus(t *testing.T) {
	t.Run("a nil state", func(t *testing.T) {
		if got := exitStatus(nil); got != 0 {
			t.Errorf("exit status = %d, want 0", got)
		}
	})
	t.Run("a plain exit", func(t *testing.T) {
		if got := exitStatus(stateOf(t, "exit 7")); got != 7 {
			t.Errorf("exit status = %d, want 7", got)
		}
	})
	t.Run("a SIGTERM death", func(t *testing.T) {
		if got := exitStatus(stateOf(t, "kill -TERM $$")); got != 143 {
			t.Errorf("exit status = %d, want 143", got)
		}
	})
	t.Run("a SIGKILL death", func(t *testing.T) {
		if got := exitStatus(stateOf(t, "kill -KILL $$")); got != 137 {
			t.Errorf("exit status = %d, want 137", got)
		}
	})
}

func TestExitStatusFromWait(t *testing.T) {
	cases := []struct {
		name   string
		status syscall.WaitStatus
		want   int
	}{
		{"a clean exit", 0x0000, 0},
		{"a failing exit", 0x0700, 7},
		{"death by SIGTERM", 0x000f, 143},
		{"death by SIGKILL", 0x0009, 137},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := exitStatusFromWait(c.status); got != c.want {
				t.Errorf("status = %d, want %d", got, c.want)
			}
		})
	}
}
