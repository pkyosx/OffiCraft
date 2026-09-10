package main

// dal_custom_themes_test.go — the per-theme table's behaviour: what a saved
// theme reads back as, which writes the layer refuses, and what a refusal
// leaves in the table.

import (
	"errors"
	"reflect"
	"testing"
)

func TestListCustomThemes(t *testing.T) {
	t.Run("an empty table reads back as nil, a populated one as its rows", func(t *testing.T) {
		d := newAPITestDAL(t)
		got, err := d.ListCustomThemes()
		if err != nil {
			t.Fatalf("ListCustomThemes on an empty table: %v", err)
		}
		if got != nil {
			t.Fatalf("ListCustomThemes on an empty table: want nil, got %+v", got)
		}

		dalSeedTheme(t, d, "dusk", 0, 1700000001)
		dalSeedTheme(t, d, "dawn", 1, 1700000002)
		got, err = d.ListCustomThemes()
		if err != nil {
			t.Fatalf("ListCustomThemes: %v", err)
		}
		want := []CustomTheme{
			{ID: "dusk", Bundle: dalThemeBundle("dusk"), OrderIdx: 0, UpdatedAt: 1700000001},
			{ID: "dawn", Bundle: dalThemeBundle("dawn"), OrderIdx: 1, UpdatedAt: 1700000002},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListCustomThemes:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the order is the stored order_idx even when it contradicts insertion order", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalSeedTheme(t, d, "first-inserted", 7, 1700000001)
		dalSeedTheme(t, d, "second-inserted", 2, 1700000002)
		dalSeedTheme(t, d, "third-inserted", 5, 1700000003)

		got, err := d.ListCustomThemes()
		if err != nil {
			t.Fatalf("ListCustomThemes: %v", err)
		}
		var ids []string
		for _, th := range got {
			ids = append(ids, th.ID)
		}
		want := []string{"second-inserted", "third-inserted", "first-inserted"}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("ListCustomThemes order: want %v, got %v", want, ids)
		}
	})
}

func TestGetCustomTheme(t *testing.T) {
	d := newAPITestDAL(t)
	dalSeedTheme(t, d, "dusk", 3, 1700000001)
	dalSeedTheme(t, d, "dawn", 4, 1700000002)

	t.Run("a stored theme reads back whole", func(t *testing.T) {
		got, err := d.GetCustomTheme("dusk")
		if err != nil {
			t.Fatalf("GetCustomTheme(dusk): %v", err)
		}
		if got == nil {
			t.Fatalf("GetCustomTheme(dusk): no row")
		}
		want := CustomTheme{ID: "dusk", Bundle: dalThemeBundle("dusk"), OrderIdx: 3, UpdatedAt: 1700000001}
		if !reflect.DeepEqual(*got, want) {
			t.Fatalf("GetCustomTheme(dusk):\n got %+v\nwant %+v", *got, want)
		}
	})

	t.Run("an id no theme carries is nil rather than an error", func(t *testing.T) {
		got, err := d.GetCustomTheme("never-saved")
		if err != nil {
			t.Fatalf("GetCustomTheme(never-saved): %v", err)
		}
		if got != nil {
			t.Fatalf("GetCustomTheme(never-saved): want nil, got %+v", *got)
		}
	})
}

func TestPutCustomTheme(t *testing.T) {
	t.Run("a new theme is appended at MAX(order_idx)+1 and stamped with the write time", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := nowSecs()
		if err := d.PutCustomTheme("dusk", dalThemeBundle("dusk")); err != nil {
			t.Fatalf("PutCustomTheme(dusk): %v", err)
		}
		if err := d.PutCustomTheme("dawn", dalThemeBundle("dawn")); err != nil {
			t.Fatalf("PutCustomTheme(dawn): %v", err)
		}
		got, err := d.ListCustomThemes()
		if err != nil {
			t.Fatalf("ListCustomThemes: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("ListCustomThemes: want two themes, got %+v", got)
		}
		for i, th := range got {
			if th.UpdatedAt < before {
				t.Fatalf("theme %d stamp %v is older than the write %v", i, th.UpdatedAt, before)
			}
			got[i].UpdatedAt = 0
		}
		want := []CustomTheme{
			{ID: "dusk", Bundle: dalThemeBundle("dusk"), OrderIdx: 0},
			{ID: "dawn", Bundle: dalThemeBundle("dawn"), OrderIdx: 1},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListCustomThemes:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("replacing a theme rewrites its bundle and stamp, keeps its position, and leaves its neighbours alone", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalSeedTheme(t, d, "dusk", 0, 1700000001)
		dalSeedTheme(t, d, "dawn", 1, 1700000002)
		dalSeedTheme(t, d, "noon", 2, 1700000003)

		edited := `{"id":"dawn","name":"Dawn, edited"}`
		before := nowSecs()
		if err := d.PutCustomTheme("dawn", edited); err != nil {
			t.Fatalf("PutCustomTheme(dawn): %v", err)
		}
		got, err := d.GetCustomTheme("dawn")
		if err != nil || got == nil {
			t.Fatalf("GetCustomTheme(dawn): %v %+v", err, got)
		}
		if got.UpdatedAt < before {
			t.Fatalf("GetCustomTheme(dawn): stamp %v is older than the write %v", got.UpdatedAt, before)
		}
		stamp := got.UpdatedAt
		want := CustomTheme{ID: "dawn", Bundle: edited, OrderIdx: 1, UpdatedAt: stamp}
		if !reflect.DeepEqual(*got, want) {
			t.Fatalf("GetCustomTheme(dawn):\n got %+v\nwant %+v", *got, want)
		}
		dalWantTheme(t, d, CustomTheme{ID: "dusk", Bundle: dalThemeBundle("dusk"), OrderIdx: 0, UpdatedAt: 1700000001})
		dalWantTheme(t, d, CustomTheme{ID: "noon", Bundle: dalThemeBundle("noon"), OrderIdx: 2, UpdatedAt: 1700000003})
	})

	t.Run("a refused write reaches the table not at all", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalSeedTheme(t, d, "dusk", 0, 1700000001)

		for _, tc := range []struct {
			name    string
			id      string
			bundle  string
			wantErr error
		}{
			{"a blank id", "", `{"id":""}`, ErrCustomThemeIDBlank},
			{"a bundle that is not JSON", "dawn", `{not json`, ErrCustomThemeBundleNotJSON},
			{"a bundle whose own id disagrees", "dawn", `{"id":"dusk"}`, ErrCustomThemeIDMismatch},
			{"a bundle carrying no id at all", "dawn", `{"name":"Dawn"}`, ErrCustomThemeIDMismatch},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if err := d.PutCustomTheme(tc.id, tc.bundle); !errors.Is(err, tc.wantErr) {
					t.Fatalf("PutCustomTheme(%q, %q): want %v, got %v", tc.id, tc.bundle, tc.wantErr, err)
				}
			})
		}

		got, err := d.ListCustomThemes()
		if err != nil {
			t.Fatalf("ListCustomThemes: %v", err)
		}
		want := []CustomTheme{{ID: "dusk", Bundle: dalThemeBundle("dusk"), OrderIdx: 0, UpdatedAt: 1700000001}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("the table after four refusals:\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestCheckCustomThemeIDMatchesBundle(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("each refusal names its own field", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			id     string
			bundle string
			want   string
		}{
			{"a blank id", "", `{"id":"dusk"}`, "custom theme: the id is blank"},
			{"a bundle that is not JSON", "dusk", `{not json`, "custom theme: the bundle is not valid JSON"},
			{"a bundle carrying no id", "dusk", `{"name":"Dusk"}`,
				"custom theme: the bundle's own id does not match the id it is being filed under: the bundle carries no id"},
			{"a bundle whose id disagrees", "dusk", `{"id":"dawn"}`,
				`custom theme: the bundle's own id does not match the id it is being filed under: bundle says "dawn", filed under "dusk"`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				err := d.checkCustomThemeIDMatchesBundle(tc.id, tc.bundle)
				if err == nil {
					t.Fatalf("checkCustomThemeIDMatchesBundle(%q, %q): want a refusal, got nil", tc.id, tc.bundle)
				}
				if err.Error() != tc.want {
					t.Fatalf("checkCustomThemeIDMatchesBundle(%q, %q):\n got %q\nwant %q", tc.id, tc.bundle, err.Error(), tc.want)
				}
			})
		}
	})

	t.Run("a matching pair passes", func(t *testing.T) {
		if err := d.checkCustomThemeIDMatchesBundle("dusk", `{"id":"dusk","name":"Dusk"}`); err != nil {
			t.Fatalf("checkCustomThemeIDMatchesBundle on a matching pair: %v", err)
		}
	})

	t.Run("the judgement is the database's, so a duplicate key resolves to the FIRST value", func(t *testing.T) {
		bundle := `{"id":"dusk","id":"dawn"}`
		if err := d.checkCustomThemeIDMatchesBundle("dusk", bundle); err != nil {
			t.Fatalf("a duplicate id key filed under the first value: %v", err)
		}
		err := d.checkCustomThemeIDMatchesBundle("dawn", bundle)
		if err == nil {
			t.Fatalf("a duplicate id key filed under the LAST value: want a refusal, got nil")
		}
		want := `custom theme: the bundle's own id does not match the id it is being filed under: bundle says "dusk", filed under "dawn"`
		if err.Error() != want {
			t.Fatalf("a duplicate id key filed under the LAST value:\n got %q\nwant %q", err.Error(), want)
		}
	})

	t.Run("a numeric id is judged as the text the column would store, while the message quotes the unconverted value", func(t *testing.T) {
		if err := d.checkCustomThemeIDMatchesBundle("1.0", `{"id":1.0}`); err != nil {
			t.Fatalf("checkCustomThemeIDMatchesBundle(1.0): %v", err)
		}
		err := d.checkCustomThemeIDMatchesBundle("1", `{"id":1.0}`)
		if err == nil {
			t.Fatalf(`{"id":1.0} filed under "1": want a refusal, got nil`)
		}
		want := `custom theme: the bundle's own id does not match the id it is being filed under: bundle says "1", filed under "1"`
		if err.Error() != want {
			t.Fatalf(`{"id":1.0} filed under "1":`+"\n got %q\nwant %q", err.Error(), want)
		}
	})
}

func TestDeleteCustomTheme(t *testing.T) {
	d := newAPITestDAL(t)
	dalSeedTheme(t, d, "dusk", 0, 1700000001)
	dalSeedTheme(t, d, "dawn", 1, 1700000002)
	dalSeedTheme(t, d, "noon", 2, 1700000003)

	t.Run("removing a stored theme reports true and leaves the survivors' positions sparse but untouched", func(t *testing.T) {
		removed, err := d.DeleteCustomTheme("dawn")
		if err != nil {
			t.Fatalf("DeleteCustomTheme(dawn): %v", err)
		}
		if !removed {
			t.Fatalf("DeleteCustomTheme(dawn): want true, got false")
		}
		got, err := d.ListCustomThemes()
		if err != nil {
			t.Fatalf("ListCustomThemes: %v", err)
		}
		want := []CustomTheme{
			{ID: "dusk", Bundle: dalThemeBundle("dusk"), OrderIdx: 0, UpdatedAt: 1700000001},
			{ID: "noon", Bundle: dalThemeBundle("noon"), OrderIdx: 2, UpdatedAt: 1700000003},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListCustomThemes after the delete:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("removing it again reports false and removes nothing else", func(t *testing.T) {
		removed, err := d.DeleteCustomTheme("dawn")
		if err != nil {
			t.Fatalf("DeleteCustomTheme(dawn) again: %v", err)
		}
		if removed {
			t.Fatalf("DeleteCustomTheme(dawn) again: want false, got true")
		}
		n, err := d.CountCustomThemes()
		if err != nil {
			t.Fatalf("CountCustomThemes: %v", err)
		}
		if n != 2 {
			t.Fatalf("CountCustomThemes after the no-op delete: want 2, got %d", n)
		}
	})
}

func TestCountCustomThemes(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.CountCustomThemes()
	if err != nil {
		t.Fatalf("CountCustomThemes on an empty table: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountCustomThemes on an empty table: want 0, got %d", got)
	}

	for i, id := range []string{"dusk", "dawn", "noon"} {
		dalSeedTheme(t, d, id, i, 1700000000+float64(i))
		got, err = d.CountCustomThemes()
		if err != nil {
			t.Fatalf("CountCustomThemes: %v", err)
		}
		if got != i+1 {
			t.Fatalf("CountCustomThemes after %d saves: want %d, got %d", i+1, i+1, got)
		}
	}
}

// dalThemeBundle is one theme's stored JSON text — the bytes this layer stores
// and returns without ever decoding them.

func dalThemeBundle(id string) string {
	return `{"id":"` + id + `","name":"Theme ` + id + `","colors":{"bg":"#101010"}}`
}

// dalSeedTheme writes one theme row directly, so a test can name a position and
// a stamp the append-only writer would never produce.

func dalSeedTheme(t *testing.T, d *DAL, id string, orderIdx int, updatedAt float64) {
	t.Helper()
	if _, err := d.wdb.Exec(
		`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, ?)`,
		id, dalThemeBundle(id), orderIdx, updatedAt); err != nil {
		t.Fatalf("seed theme %q: %v", id, err)
	}
}

func dalWantTheme(t *testing.T, d *DAL, want CustomTheme) {
	t.Helper()
	got, err := d.GetCustomTheme(want.ID)
	if err != nil {
		t.Fatalf("GetCustomTheme(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetCustomTheme(%q): no row", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetCustomTheme(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}
