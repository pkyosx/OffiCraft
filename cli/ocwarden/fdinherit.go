package main

import (
	"os"
	"strconv"
	"syscall"
)

// Go marks only the descriptors it opens itself close-on-exec. macOS's resolver
// leaves a com.apple.netsrc socket open without it when resolving the station's
// hostname, so each in-place self-update carried the socket into the new image
// (which opened another), and every child inherited the pile until `codex
// app-server` ran out of descriptors under launchd's 256 limit.
//
// Call it right before every exec and child start: the resolver can open the
// socket at any point in the process's life.
func markDescriptorsCloseOnExec() {
	dir, err := os.Open("/dev/fd")
	if err != nil {
		return
	}
	// Readdirnames, not ReadDir: ReadDir stats each entry, and on macOS that fails
	// partway through /dev/fd and drops the rest of the list.
	names, _ := dir.Readdirnames(-1)
	dir.Close()
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil || fd <= 2 {
			continue
		}
		syscall.CloseOnExec(fd)
	}
}
