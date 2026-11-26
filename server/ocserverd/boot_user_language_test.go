package main

import (
	"net/http"
	"strings"
	"testing"
)

// The 使用者語言 block, driven end to end: display.language is written through
// PATCH /api/settings and read back through the two boot-context previews
// (staff and outsource), which re-run the folds the spawn paths hand to a
// booting member. Every expected string is spelled out literally.
func TestBootContextUserLanguageBlock(t *testing.T) {
	const (
		sysBody      = "# SYSTEM INTERACTION FOR LANGUAGE TEST"
		ownerAddText = "OWNER ADDITIONS FOR LANGUAGE TEST"
		zhBlock      = "# User Language\n\n除非是技術專有名詞可以用英文，其餘預設（包含報告）用繁體中文 (台灣)。 不要用其他語言。"
		enBlock      = "# User Language\n\nUse English for everything you write to the user, including reports. Do not use other languages."
	)
	head := func(block string) string {
		return sysBody + "\n\n" + block + "\n\n# 使用者自訂（Owner Additions）\n\n" + ownerAddText + "\n\n"
	}

	api, h, d, owner := newAPITestServer(t)
	api.noOutsource = true
	memberPreviewSeed(t, d, Member{
		ID: "m-writer", Name: "Writer", RoleKey: memberPreviewRoleKey, Runtime: RuntimeClaude,
	})
	if status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"`+sysBody+`"}`); status != http.StatusOK {
		t.Fatalf("replace system interaction: %d %v", status, data)
	}
	if status, data := apiJSON(t, h, "POST", "/api/global-context", owner, `{"text":"`+ownerAddText+`"}`); status != http.StatusOK {
		t.Fatalf("replace owner additions: %d %v", status, data)
	}
	workerID := assignOneWorker(t, api)

	preview := func(path string) string {
		t.Helper()
		status, data := apiJSON(t, h, "GET", path, owner, "")
		if status != http.StatusOK {
			t.Fatalf("GET %s: %d %v", path, status, data)
		}
		ctx, _ := data["context"].(string)
		return ctx
	}
	setLanguage := func(lang string) {
		t.Helper()
		status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_language":"`+lang+`"}`)
		if status != http.StatusOK {
			t.Fatalf("PATCH display_language=%q: %d %v", lang, status, data)
		}
	}
	check := func(label, wantBlock, otherBlock string) {
		t.Helper()
		staff := preview("/api/members/m-writer/boot-context")
		worker := preview("/api/outsource-workers/" + workerID + "/boot-context")
		for _, c := range []struct{ who, ctx, tail string }{
			{"staff", staff, "\n\n" + memberPreviewClaudeBoot + "\n"},
			{"outsource", worker, "\n\n" + memberPreviewClaudeBoot + "\n"},
		} {
			if !strings.HasPrefix(c.ctx, head(wantBlock)) {
				t.Errorf("%s / %s: boot context must open with system interaction, then the "+
					"language block, then owner additions; got head %q",
					label, c.who, c.ctx[:min(len(c.ctx), len(head(wantBlock))+40)])
			}
			if n := strings.Count(c.ctx, "# User Language"); n != 1 {
				t.Errorf("%s / %s: want exactly one language block, found %d", label, c.who, n)
			}
			if strings.Contains(c.ctx, otherBlock) {
				t.Errorf("%s / %s: carries the other language's block", label, c.who)
			}
			if !strings.HasSuffix(c.ctx, c.tail) {
				t.Errorf("%s / %s: boot sequence is no longer the tail", label, c.who)
			}
		}
		// Staff and outsource share this head byte for byte.
		if !strings.HasPrefix(staff, head(wantBlock)) || !strings.HasPrefix(worker, head(wantBlock)) {
			t.Errorf("%s: staff and outsource heads differ", label)
		}
	}

	check("unset", zhBlock, enBlock)
	setLanguage("en")
	check("en", enBlock, zhBlock)
	setLanguage("zh")
	check("zh", zhBlock, enBlock)
	setLanguage("en")
	check("en again", enBlock, zhBlock)
	setLanguage("")
	check("cleared", zhBlock, enBlock)
}

// Without owner additions the language block sits directly before the persona
// (staff) and is still present for outsource; the blank 使用者自訂 heading must
// not reappear.
func TestBootContextUserLanguageBlockWithoutOwnerAdditions(t *testing.T) {
	const sysBody = "# SYSTEM INTERACTION FOR LANGUAGE TEST"
	api, h, d, owner := newAPITestServer(t)
	api.noOutsource = true
	memberPreviewSeed(t, d, Member{
		ID: "m-writer", Name: "Writer", RoleKey: memberPreviewRoleKey, Runtime: RuntimeClaude,
	})
	if status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"`+sysBody+`"}`); status != http.StatusOK {
		t.Fatalf("replace system interaction: %d %v", status, data)
	}
	if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"display_language":"en"}`); status != http.StatusOK {
		t.Fatalf("PATCH display_language: %d %v", status, data)
	}
	workerID := assignOneWorker(t, api)

	_, staff := apiJSON(t, h, "GET", "/api/members/m-writer/boot-context", owner, "")
	_, worker := apiJSON(t, h, "GET", "/api/outsource-workers/"+workerID+"/boot-context", owner, "")
	staffCtx, _ := staff["context"].(string)
	workerCtx, _ := worker["context"].(string)

	wantStaff := sysBody + "\n\n# User Language\n\nUse English for everything you write to the user, including reports. Do not use other languages.\n\n# Role: Preview Writer\n\n# Preview role instructions\n\n"
	if !strings.HasPrefix(staffCtx, wantStaff) {
		t.Errorf("staff head = %q, want prefix %q", staffCtx[:min(len(staffCtx), len(wantStaff)+40)], wantStaff)
	}
	wantWorker := sysBody + "\n\n# User Language\n\nUse English for everything you write to the user, including reports. Do not use other languages.\n\n"
	if !strings.HasPrefix(workerCtx, wantWorker) {
		t.Errorf("outsource head = %q, want prefix %q", workerCtx[:min(len(workerCtx), len(wantWorker)+40)], wantWorker)
	}
	for who, ctx := range map[string]string{"staff": staffCtx, "outsource": workerCtx} {
		if strings.Contains(ctx, "# 使用者自訂（Owner Additions）") {
			t.Errorf("%s: blank owner additions produced a heading", who)
		}
	}
}
