package main

import (
	"context"
	"testing"
	"time"
)

func TestUpReaimCustomCursors(t *testing.T) {
	const (
		slotLayout = "2006-01-02T15:04-07:00"
		allMonths  = "1,2,3,4,5,6,7,8,9,10,11,12"
		allDays    = "1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31"
		allHours   = "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23"
		allMinute  = "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59"
	)

	t.Run("aims a live custom schedule at the current elapsed slot", func(t *testing.T) {
		d := newAPITestDAL(t)
		insertMigrationSchedule(t, d, migrationScheduleSeed{
			id:            "sch-live-custom",
			cadence:       "custom",
			months:        allMonths,
			days:          allDays,
			hours:         allHours,
			minutes:       allMinute,
			timezone:      "Asia/Taipei",
			lastFiredSlot: "",
		})

		before := time.Now()
		runReaimCustomCursors(t, d)
		after := time.Now()

		got := migrationScheduleStateOf(t, d, "sch-live-custom")
		if got.lastFiredSlot == "" {
			t.Fatal("live custom schedule cursor is empty after migration")
		}
		parsed, err := time.Parse(slotLayout, got.lastFiredSlot)
		if err != nil {
			t.Fatalf("live custom schedule cursor = %q, want a slot key: %v", got.lastFiredSlot, err)
		}
		if parsed.Format(slotLayout) != got.lastFiredSlot {
			t.Fatalf("live custom schedule cursor = %q, want canonical slot key", got.lastFiredSlot)
		}
		if parsed.Format("-07:00") != "+08:00" {
			t.Fatalf("live custom schedule cursor offset = %q, want +08:00", parsed.Format("-07:00"))
		}
		beforeKey := before.In(parsed.Location()).Format(slotLayout)
		afterKey := after.In(parsed.Location()).Format(slotLayout)
		if got.lastFiredSlot != beforeKey && got.lastFiredSlot != afterKey {
			t.Fatalf("live custom schedule cursor = %q, want the current Taipei minute (%q or %q)", got.lastFiredSlot, beforeKey, afterKey)
		}
		if got.lastFiredTS != 0 {
			t.Fatalf("live custom schedule last_fired_ts = %v, want 0", got.lastFiredTS)
		}
		if got.cadence != "custom" || got.timezone != "Asia/Taipei" {
			t.Fatalf("live custom schedule identity changed: cadence=%q timezone=%q", got.cadence, got.timezone)
		}
		if got.months != allMonths || got.days != allDays || got.hours != allHours || got.minutes != allMinute {
			t.Fatalf("live custom schedule sets changed: months=%q days=%q hours=%q minutes=%q", got.months, got.days, got.hours, got.minutes)
		}
	})

	t.Run("leaves already aimed and non-eligible rows unchanged", func(t *testing.T) {
		d := newAPITestDAL(t)
		seeds := []migrationScheduleSeed{
			{
				id:            "sch-existing-cursor",
				cadence:       "custom",
				months:        allMonths,
				days:          allDays,
				hours:         allHours,
				minutes:       allMinute,
				timezone:      "Asia/Taipei",
				lastFiredSlot: "2026-08-01T00:00+08:00",
				lastFiredTS:   17.25,
			},
			{
				id:       "sch-parked-daily",
				cadence:  "daily",
				months:   allMonths,
				days:     allDays,
				hours:    allHours,
				minutes:  allMinute,
				timezone: "Asia/Taipei",
			},
			{
				id:       "sch-invalid-zone",
				cadence:  "custom",
				months:   allMonths,
				days:     allDays,
				hours:    allHours,
				minutes:  allMinute,
				timezone: "Not/A/Real/Zone",
			},
			{
				id:       "sch-impossible-date",
				cadence:  "custom",
				months:   "2",
				days:     "30",
				hours:    "0",
				minutes:  "0",
				timezone: "Asia/Taipei",
			},
		}
		for _, seed := range seeds {
			insertMigrationSchedule(t, d, seed)
		}

		runReaimCustomCursors(t, d)

		for _, seed := range seeds {
			seed := seed
			t.Run(seed.id, func(t *testing.T) {
				got := migrationScheduleStateOf(t, d, seed.id)
				if got.lastFiredSlot != seed.lastFiredSlot {
					t.Fatalf("last_fired_slot = %q, want %q", got.lastFiredSlot, seed.lastFiredSlot)
				}
				if got.lastFiredTS != seed.lastFiredTS {
					t.Fatalf("last_fired_ts = %v, want %v", got.lastFiredTS, seed.lastFiredTS)
				}
				if got.cadence != seed.cadence || got.timezone != seed.timezone {
					t.Fatalf("schedule identity = (%q, %q), want (%q, %q)", got.cadence, got.timezone, seed.cadence, seed.timezone)
				}
				if got.months != seed.months || got.days != seed.days || got.hours != seed.hours || got.minutes != seed.minutes {
					t.Fatalf("schedule sets = (%q, %q, %q, %q), want (%q, %q, %q, %q)", got.months, got.days, got.hours, got.minutes, seed.months, seed.days, seed.hours, seed.minutes)
				}
			})
		}
	})
}

type migrationScheduleSeed struct {
	id            string
	cadence       string
	months        string
	days          string
	hours         string
	minutes       string
	timezone      string
	lastFiredSlot string
	lastFiredTS   float64
}

type migrationScheduleState struct {
	cadence       string
	months        string
	days          string
	hours         string
	minutes       string
	timezone      string
	lastFiredSlot string
	lastFiredTS   float64
}

func insertMigrationSchedule(t *testing.T, d *DAL, seed migrationScheduleSeed) {
	t.Helper()
	_, err := d.wdb.Exec(`
		INSERT INTO scheduled_message (
			id, member_id, label, body, cadence,
			custom_months, custom_days, custom_hours, custom_minutes,
			timezone, last_fired_slot, last_fired_ts
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.id, "member-migration-test", "migration test", "migration body", seed.cadence,
		seed.months, seed.days, seed.hours, seed.minutes,
		seed.timezone, seed.lastFiredSlot, seed.lastFiredTS)
	if err != nil {
		t.Fatalf("insert schedule %q: %v", seed.id, err)
	}
}

func runReaimCustomCursors(t *testing.T, d *DAL) {
	t.Helper()
	tx, err := d.wdb.Begin()
	if err != nil {
		t.Fatalf("begin migration transaction: %v", err)
	}
	if err := upReaimCustomCursors(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("upReaimCustomCursors: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration transaction: %v", err)
	}
}

func migrationScheduleStateOf(t *testing.T, d *DAL, id string) migrationScheduleState {
	t.Helper()
	var got migrationScheduleState
	err := d.wdb.QueryRow(`
		SELECT cadence, custom_months, custom_days, custom_hours, custom_minutes,
		       timezone, last_fired_slot, last_fired_ts
		  FROM scheduled_message WHERE id = ?`, id).
		Scan(&got.cadence, &got.months, &got.days, &got.hours, &got.minutes,
			&got.timezone, &got.lastFiredSlot, &got.lastFiredTS)
	if err != nil {
		t.Fatalf("read schedule %q: %v", id, err)
	}
	return got
}
