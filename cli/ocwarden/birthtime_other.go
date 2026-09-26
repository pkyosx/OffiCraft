//go:build !darwin

package main

import "time"

// mtime is NOT a substitute for birth time: it moves on any write and is
// settable, which would turn a deterministic negative into a guess. Refusing
// only drops the verdict's negative direction.
func statBirthTime(string) (time.Time, error) {
	return time.Time{}, errNoBirthTime
}
