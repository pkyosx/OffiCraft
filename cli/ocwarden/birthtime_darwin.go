//go:build darwin

package main

import (
	"os"
	"syscall"
	"time"
)

// Only macOS has the anchor identity (TCC) question, so the real implementation
// lives here.
func statBirthTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, errNoBirthTime
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec), nil
}
