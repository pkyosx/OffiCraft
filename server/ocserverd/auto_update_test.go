// Skeleton generated from server/ocserverd/auto_update.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"testing"
	"time"
)

func TestAutoUpdateEnabled(t *testing.T) {
	api := &apiServer{}
	if api.autoUpdateEnabled() {
		t.Fatal("auto-update is enabled before the toggle is armed")
	}

	api.updaterAutoUpdate = true
	if !api.autoUpdateEnabled() {
		t.Fatal("auto-update is disabled after the toggle is armed")
	}
}

func TestStartAutoUpdateCadence(t *testing.T) {
	api := &apiServer{}
	api.startAutoUpdateCadence(time.Hour)
}

func TestAutoUpdateTick(t *testing.T) {
	t.Run("disabled auto-update does not evaluate a release", func(t *testing.T) {
		api := &apiServer{}
		if api.autoUpdateTick() {
			t.Fatal("disabled auto-update acted")
		}
	})

	t.Run("armed auto-update with no newer cached release does nothing", func(t *testing.T) {
		api := &apiServer{updaterAutoUpdate: true}
		api.updateCheck = updateCheckState{checkedAt: time.Now()}
		if api.autoUpdateTick() {
			t.Fatal("auto-update acted without a newer cached release")
		}
	})
}

func TestDerefOr(t *testing.T) {
	var absent *string
	if got := derefOr(absent, "unknown"); got != "unknown" {
		t.Fatalf("nil value = %q, want %q", got, "unknown")
	}

	value := "v1.2.3"
	if got := derefOr(&value, "unknown"); got != "v1.2.3" {
		t.Fatalf("present value = %q, want %q", got, "v1.2.3")
	}
}
