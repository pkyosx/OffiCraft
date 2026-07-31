package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestSaveWithDocumentHistoryKeepsThreePreWriteSnapshots(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	dal := NewDAL(db)
	if err := dal.PutUserContext(UserContext{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	snapshot := func(q sqlQuerier) (string, error) {
		current, err := getUserContextOn(q)
		if err != nil {
			return "", err
		}
		return `{"text":"` + current.Text + `"}`, nil
	}
	for _, next := range []string{"two", "three", "four", "five"} {
		if err := dal.SaveWithDocumentHistory("global_context", "global", "owner", snapshot, func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: next})
		}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history count = %d, want 3", len(history))
	}
	for i, want := range []string{"four", "three", "two"} {
		if got := history[i].ContentJSON; got != `{"text":"`+want+`"}` {
			t.Errorf("history[%d] = %s, want %s", i, got, want)
		}
	}
	current, err := dal.GetUserContext()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "five" {
		t.Fatalf("live text = %q, want five", current.Text)
	}
}

// The revision chain must survive a writer that folded its replacement from a
// read taken before someone else's write landed. Two callers reading "one" and
// then writing in turn used to retain "one" twice, leaving the value written in
// between recoverable from nowhere.
func TestSaveWithDocumentHistoryRetainsTheValueItActuallyReplaced(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	dal := NewDAL(db)
	if err := dal.PutUserContext(UserContext{Text: "one"}); err != nil {
		t.Fatal(err)
	}

	// Both writers read the document here, while it still says "one".
	slow, err := dal.GetUserContext()
	if err != nil {
		t.Fatal(err)
	}
	if slow.Text != "one" {
		t.Fatalf("baseline read = %q, want one — the fixture never set up the race", slow.Text)
	}

	write := func(next string) {
		t.Helper()
		if err := dal.SaveWithDocumentHistory("global_context", "global", "owner", userContextSnapshotIn, func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: next})
		}); err != nil {
			t.Fatal(err)
		}
	}
	write("two")   // the fast writer commits first
	write("three") // the slow writer, still holding the stale "one", writes next

	history, err := dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history count = %d, want 2", len(history))
	}
	var texts []string
	for _, h := range history {
		texts = append(texts, h.ContentJSON)
	}
	if texts[0] != `{"text":"two","tombstoned":"false"}` {
		t.Errorf("newest revision = %s, want the replaced value \"two\"", texts[0])
	}
	if texts[1] != `{"text":"one","tombstoned":"false"}` {
		t.Errorf("oldest revision = %s, want \"one\"", texts[1])
	}
}

// Concurrent writers must leave a contiguous chain: the retained revisions are
// the values immediately preceding the live one, each retained once. A snapshot
// read taken outside the write's own transaction lets two writers retain the
// same ancestor, which shows up here as a duplicate revision and a value that
// was live but is recoverable from nowhere.
func TestSaveWithDocumentHistoryUnderConcurrentWritersKeepsTheChainContiguous(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	dal := NewDAL(db)
	if err := dal.PutUserContext(UserContext{Text: "seed"}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var committed []string // the order the writes actually landed in
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next := fmt.Sprintf("v%d", i)
			if err := dal.SaveWithDocumentHistory("global_context", "global", "owner", userContextSnapshotIn, func(ex sqlExecer) error {
				if err := putUserContextOn(ex, UserContext{Text: next}); err != nil {
					return err
				}
				mu.Lock()
				committed = append(committed, next)
				mu.Unlock()
				return nil
			}); err != nil {
				t.Errorf("write %s: %v", next, err)
			}
		}()
	}
	wg.Wait()

	history, err := dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != documentHistoryKeep {
		t.Fatalf("history count = %d, want %d", len(history), documentHistoryKeep)
	}
	var retained []string
	seen := map[string]bool{}
	for _, h := range history {
		var content map[string]string
		if err := json.Unmarshal([]byte(h.ContentJSON), &content); err != nil {
			t.Fatal(err)
		}
		if seen[content["text"]] {
			t.Fatalf("revision %q retained twice — two writers snapshotted the same ancestor: %v",
				content["text"], history)
		}
		seen[content["text"]] = true
		retained = append(retained, content["text"])
	}
	// history is newest-first; the commit order's last three values are the live
	// one and the two revisions before it.
	want := []string{
		committed[len(committed)-2],
		committed[len(committed)-3],
		committed[len(committed)-4],
	}
	for i, w := range want {
		if retained[i] != w {
			t.Fatalf("retained = %v, want %v (commit order %v)", retained, want, committed)
		}
	}
	current, err := dal.GetUserContext()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != committed[len(committed)-1] {
		t.Fatalf("live text = %q, want %q", current.Text, committed[len(committed)-1])
	}
}

func newHistoryDAL(t *testing.T) *DAL {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	return NewDAL(db)
}

// retainOneVersion writes the document twice so it exists AND has exactly one
// retained revision — the two rows a delete has to remove together.
func retainOneVersion(t *testing.T, d *DAL, kind, key string, write func(sqlExecer) error) {
	t.Helper()
	blank := func(sqlQuerier) (string, error) { return "{}", nil }
	previous := func(sqlQuerier) (string, error) { return `{"text":"previous"}`, nil }
	if err := d.SaveWithDocumentHistory(kind, key, "owner", blank, write); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveWithDocumentHistory(kind, key, "owner", previous, write); err != nil {
		t.Fatal(err)
	}
	history, err := d.ListDocumentHistory(kind, key)
	if err != nil || len(history) != 1 {
		t.Fatalf("seed %s/%s history = %+v, %v; want exactly one revision", kind, key, history, err)
	}
}

func refuseDeletesOn(t *testing.T, d *DAL, table string) {
	t.Helper()
	if _, err := d.wdb.Exec(`CREATE TRIGGER refuse_delete_` + table +
		` BEFORE DELETE ON ` + table +
		` BEGIN SELECT RAISE(ABORT, 'delete refused'); END`); err != nil {
		t.Fatal(err)
	}
}

// A delete that drops the document but keeps its retained revisions (or the
// reverse) is the half-applied state the cascade exists to prevent, and neither
// half is observable from a successful call. So each half is driven with the
// OTHER half rigged to fail: whatever survives must be both rows or neither.
func TestDeletingADocumentAndItsRetainedHistoryIsAllOrNothing(t *testing.T) {
	const roleKey, typeKey = "r-doomed", "tm-doomed"
	lessonsKey := roleKey + "::" + seedLessonsTaskType
	for _, doc := range []struct {
		name       string
		table      string
		kind, key  string
		seed       func(*testing.T, *DAL)
		remove     func(*DAL) error
		documentIn func(*testing.T, *DAL) bool
	}{
		{
			name: "role definition", table: "role_def", kind: "role_definition", key: roleKey,
			seed: func(t *testing.T, d *DAL) {
				retainOneVersion(t, d, "role_definition", roleKey, func(ex sqlExecer) error {
					return putRoleDefOn(ex, RoleDef{RoleKey: roleKey, Name: "doomed", DefinitionMD: "live"})
				})
			},
			remove: func(d *DAL) error { _, err := d.DeleteRoleDef(roleKey); return err },
			documentIn: func(t *testing.T, d *DAL) bool {
				rd, err := d.GetRoleDef(roleKey)
				if err != nil {
					t.Fatal(err)
				}
				return rd != nil
			},
		},
		{
			name: "lessons", table: "lessons", kind: "lessons", key: lessonsKey,
			seed: func(t *testing.T, d *DAL) {
				retainOneVersion(t, d, "lessons", lessonsKey, func(ex sqlExecer) error {
					return putLessonsOn(ex, Lessons{RoleKey: roleKey, TaskType: seedLessonsTaskType, Text: "live"})
				})
			},
			remove: func(d *DAL) error { _, err := d.DeleteLessonsForRole(roleKey); return err },
			documentIn: func(t *testing.T, d *DAL) bool {
				l, err := d.GetLessons(roleKey, seedLessonsTaskType)
				if err != nil {
					t.Fatal(err)
				}
				return l != nil
			},
		},
		{
			name: "task manual", table: "task_manual", kind: docKindTaskManualLearnings, key: typeKey,
			seed: func(t *testing.T, d *DAL) {
				retainOneVersion(t, d, docKindTaskManualLearnings, typeKey, func(ex sqlExecer) error {
					return putTaskManualOn(ex, TaskManual{
						TypeKey: typeKey, DisplayName: "doomed", Fields: "[]",
						Assignee: "{}", Learnings: "live", UpdatedTS: nowSecs(),
					})
				})
			},
			remove: func(d *DAL) error { _, err := d.DeleteTaskManual(typeKey); return err },
			documentIn: func(t *testing.T, d *DAL) bool {
				m, err := d.GetTaskManual(typeKey)
				if err != nil {
					t.Fatal(err)
				}
				return m != nil
			},
		},
	} {
		for _, failing := range []string{"document_history", doc.table} {
			t.Run(doc.name+"/"+failing+" delete refused", func(t *testing.T) {
				d := newHistoryDAL(t)
				doc.seed(t, d)
				refuseDeletesOn(t, d, failing)

				if err := doc.remove(d); err == nil {
					t.Fatalf("%s: deleting with %s refusing every DELETE reported success",
						doc.name, failing)
				}
				if !doc.documentIn(t, d) {
					t.Errorf("%s: the document is gone but deleting %s failed — "+
						"the two deletes are not one transaction", doc.name, failing)
				}
				history, err := d.ListDocumentHistory(doc.kind, doc.key)
				if err != nil {
					t.Fatal(err)
				}
				if len(history) != 1 {
					t.Errorf("%s: retained history = %d revisions but deleting %s failed — "+
						"the two deletes are not one transaction", doc.name, len(history), failing)
				}
			})
		}
	}
}
