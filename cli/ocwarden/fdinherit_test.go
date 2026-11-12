package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// openInheritableDescriptor stands in for the resolver's socket: syscall.Open,
// unlike os.Open, does not add O_CLOEXEC.
func openInheritableDescriptor(t *testing.T) int {
	t.Helper()
	opened, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer syscall.Close(opened)
	// A high number, so the child shell's own descriptors cannot answer for it.
	const fd = 77
	if err := syscall.Dup2(opened, fd); err != nil {
		t.Fatalf("dup2: %v", err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	return fd
}

func descriptorsSeenByChild(t *testing.T, fds ...int) string {
	t.Helper()
	var script strings.Builder
	for _, fd := range fds {
		n := strconv.Itoa(fd)
		fmt.Fprintf(&script, "if [ -e /dev/fd/%[1]s ]; then echo %[1]s=open; else echo %[1]s=closed; fi;", n)
	}
	out, err := exec.Command("/bin/sh", "-c", script.String()).Output()
	if err != nil {
		t.Fatalf("child shell: %v", err)
	}
	return string(out)
}

func closeOnExec(t *testing.T, fd int) bool {
	t.Helper()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if errno != 0 {
		t.Fatalf("fcntl(%d, F_GETFD): %v", fd, errno)
	}
	return flags&syscall.FD_CLOEXEC != 0
}

func TestMarkDescriptorsCloseOnExec(t *testing.T) {
	t.Run("a child started afterwards no longer inherits a descriptor opened without close-on-exec", func(t *testing.T) {
		fd := openInheritableDescriptor(t)
		n := strconv.Itoa(fd)
		if got := descriptorsSeenByChild(t, fd); got != n+"=open\n" {
			t.Fatalf("before marking the child saw %q, want %q", got, n+"=open\n")
		}

		markDescriptorsCloseOnExec()

		if got := descriptorsSeenByChild(t, fd); got != n+"=closed\n" {
			t.Errorf("after marking the child saw %q, want %q", got, n+"=closed\n")
		}
	})

	t.Run("standard input, output and error stay inheritable and the marked descriptor stays open", func(t *testing.T) {
		fd := openInheritableDescriptor(t)

		markDescriptorsCloseOnExec()

		for _, std := range []int{0, 1, 2} {
			if closeOnExec(t, std) {
				t.Errorf("descriptor %d is close-on-exec, want it carried across exec", std)
			}
		}
		var buf [1]byte
		if n, err := syscall.Read(fd, buf[:]); n != 0 || err != nil {
			t.Errorf("read on the marked descriptor = (%d, %v), want (0, nil) at /dev/null's end", n, err)
		}
	})
}
