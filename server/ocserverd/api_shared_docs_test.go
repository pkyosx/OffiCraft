package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func apiTestUploadBlob(t *testing.T, h http.Handler, token, filename, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/chat/attachments?filename="+filename, token, body)
	id, _ := data["id"].(string)
	if status != 200 || id == "" {
		t.Fatalf("upload %s: %d %v", filename, status, data)
	}
	return id
}

func apiTestCreateSharedDoc(t *testing.T, h http.Handler, token, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/shared-docs", token, body)
	id, _ := data["id"].(string)
	if status != 200 || !strings.HasPrefix(id, "sd-") {
		t.Fatalf("create shared doc: %d %v", status, data)
	}
	return id
}

func apiTestSharedDocFile(t *testing.T, h http.Handler, token, name, filename, content string) (string, string) {
	t.Helper()
	att := apiTestUploadBlob(t, h, token, filename, content)
	return apiTestCreateSharedDoc(t, h, token,
		`{"kind":"file","name":"`+name+`","attachment_id":"`+att+`"}`), att
}

// A remove frame carries no payload: the hub drops it on every remove.
func apiTestSharedDocFrame(seq, epoch int, op, id, trigger string) map[string]any {
	var payload any = map[string]any{"id": id}
	if op == "remove" {
		payload = nil
	}
	return map[string]any{
		"seq":   seq,
		"topic": "shared_doc",
		"op":    op,
		"data": map[string]any{
			"entity":  "shared_doc",
			"key":     "owner::" + id,
			"epoch":   epoch,
			"deleted": op == "remove",
			"payload": payload,
		},
		"ts":      apiAnyNumber,
		"trigger": trigger,
	}
}

func apiTestSharedDocRow(over map[string]any) map[string]any {
	row := map[string]any{
		"id":            apiAnyString,
		"kind":          "file",
		"attachment_id": apiAnyString,
		"name":          "Runbook",
		"description":   "",
		"filename":      "runbook.md",
		"mime":          "text/markdown",
		"url":           apiAnyString,
		"created_ts":    apiAnyNumber,
		"created_by":    "kip",
		"updated_ts":    apiAnyNumber,
		"updated_by":    "kip",
		"archived_ts":   nil,
		"archived_by":   nil,
		"version_count": 1,
	}
	for k, v := range over {
		row[k] = v
	}
	return row
}

func apiTestSharedDocReceipt(id string, versionCount int, archivedTS any) map[string]any {
	return map[string]any{
		"id": id, "updated_ts": apiAnyNumber, "version_count": versionCount,
		"archived_ts": archivedTS,
	}
}

func TestHandleListSharedDocsApiSharedDocsGet(t *testing.T) {
	t.Run("a machine reads the active documents sorted by name; include_archived=true adds the archived ones", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		zed, _ := apiTestSharedDocFile(t, h, agent, "Zed", "runbook.md", "# z")
		alpha, _ := apiTestSharedDocFile(t, h, agent, "Alpha", "runbook.md", "# a")
		link := apiTestCreateSharedDoc(t, h, agent,
			`{"kind":"link","name":"Board","description":"the roadmap","url":"https://example.com/board"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+zed+"/archive", agent, "")
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/shared-docs", machine, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		alphaRow := apiTestSharedDocRow(map[string]any{"id": alpha, "name": "Alpha"})
		linkRow := apiTestSharedDocRow(map[string]any{
			"id": link, "kind": "link", "name": "Board", "description": "the roadmap",
			"filename": "", "mime": "text/uri-list", "url": "https://example.com/board",
		})
		apiWantBody(t, data, map[string]any{"docs": []any{alphaRow, linkRow}})

		for _, query := range []string{"?include_archived=1", "?include_archived=TRUE"} {
			status, data = apiJSON(t, h, "GET", "/api/shared-docs"+query, machine, "")
			if status != 200 {
				t.Fatalf("%s: want 200, got %d (%v)", query, status, data)
			}
			apiWantBody(t, data, map[string]any{"docs": []any{alphaRow, linkRow}})
		}

		status, data = apiJSON(t, h, "GET", "/api/shared-docs?include_archived=true", machine, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"docs": []any{alphaRow, linkRow,
			apiTestSharedDocRow(map[string]any{
				"id": zed, "name": "Zed", "archived_ts": apiAnyNumber, "archived_by": "kip",
			})}})
	})

	t.Run("no documents answers an empty list", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "GET", "/api/shared-docs", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"docs": []any{}})
	})
}

func TestHandleCreateSharedDocApiSharedDocsPost(t *testing.T) {
	t.Run("a member's file document is created, fanned to every connection and readable by id", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		att := apiTestUploadBlob(t, h, agent, "runbook.md", "# Ship it")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "ann")

		status, data := apiJSON(t, h, "POST", "/api/shared-docs", agent,
			`{"kind":"file","name":"  Runbook  ","description":"how we ship","attachment_id":"`+att+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		id, _ := data["id"].(string)
		apiWantBody(t, data, apiTestSharedDocReceipt(apiAnyString, 1, nil))
		dashboard.wantFrames(apiTestSharedDocFrame(1, 1, "patch", id, "kip"))
		bystander.wantFrames(apiTestSharedDocFrame(1, 1, "patch", id, "kip"))

		status, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		row := apiTestSharedDocRow(map[string]any{
			"id": id, "attachment_id": att, "description": "how we ship",
			"url": "/api/chat/attachment/" + att,
		})
		row["text"], row["text_omitted"] = "# Ship it", nil
		apiWantBody(t, data, row)
	})

	refusals := []struct {
		name, body, code, message string
		status                    int
	}{
		{"a missing name answers 422", `{"kind":"file"}`, "validation_error", "field required: name", 422},
		{"an unknown kind answers 400", `{"kind":"folder","name":"x"}`, "validation_error", "kind must be one of file, image, link", 400},
		{"a blank name answers 400", `{"kind":"link","name":"  ","url":"https://e.com"}`, "validation_error", "name is required", 400},
		{"a name over 128 chars answers 400", `{"kind":"link","name":"` + strings.Repeat("n", 129) + `","url":"https://e.com"}`,
			"validation_error", "shared doc name is 129 chars, over the 128-char limit", 400},
		{"a description over 1024 chars answers 400", `{"kind":"link","name":"x","description":"` + strings.Repeat("d", 1025) + `","url":"https://e.com"}`,
			"validation_error", "shared doc description is 1025 chars, over the 1024-char limit", 400},
		{"a link with an attachment id answers 400", `{"kind":"link","name":"x","url":"https://e.com","attachment_id":"att-1"}`,
			"validation_error", "a link shared doc does not take attachment_id: its kind is fixed, so create a new shared doc instead", 400},
		{"a link without a url answers 400", `{"kind":"link","name":"x"}`, "validation_error", "url is required for a link shared doc", 400},
		{"a link to a non-http url answers 400", `{"kind":"link","name":"x","url":"ftp://e.com"}`,
			"validation_error", "url must start with https:// or http://", 400},
		{"a file with a url answers 400", `{"kind":"file","name":"x","url":"https://e.com"}`,
			"validation_error", "a file shared doc does not take url: its kind is fixed, so create a new shared doc instead", 400},
		{"an image without an attachment id answers 400", `{"kind":"image","name":"x"}`,
			"validation_error", "attachment_id is required for an image shared doc", 400},
		{"a file without an attachment id answers 400", `{"kind":"file","name":"x"}`,
			"validation_error", "attachment_id is required for a file shared doc", 400},
		{"a member avatar answers 400", `{"kind":"image","name":"x","attachment_id":"ava-0123456789ab"}`,
			"validation_error", "attachment 'ava-0123456789ab' is reserved for a member avatar", 400},
		{"an attachment not in the store answers 400", `{"kind":"file","name":"x","attachment_id":"att-000000000000"}`,
			"validation_error", "attachment 'att-000000000000' not found (upload it first via POST /api/chat/attachments)", 400},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			api, h, _, owner := newAPITestServer(t)
			dashboard := apiTestListen(t, api, "")
			status, data := apiJSON(t, h, "POST", "/api/shared-docs", owner, tc.body)
			if status != tc.status {
				t.Fatalf("want %d, got %d (%v)", tc.status, status, data)
			}
			apiWantError(t, data, tc.code, tc.message)
			dashboard.wantFrames()
			status, data = apiJSON(t, h, "GET", "/api/shared-docs", owner, "")
			if status != 200 {
				t.Fatalf("list: %d %v", status, data)
			}
			apiWantBody(t, data, map[string]any{"docs": []any{}})
		})
	}

	t.Run("a machine identity cannot create one", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		status, data := apiJSON(t, h, "POST", "/api/shared-docs", machine,
			`{"kind":"link","name":"x","url":"https://e.com"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})
}

func TestHandleGetSharedDocApiSharedDocsDocIdGet(t *testing.T) {
	t.Run("any machine reads a text document with its content inline", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id, att := apiTestSharedDocFile(t, h, owner, "Spec", "spec.json", `{"a":1}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id, machine, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		row := apiTestSharedDocRow(map[string]any{
			"id": id, "attachment_id": att, "name": "Spec", "filename": "spec.json",
			"mime": "application/json", "url": "/api/chat/attachment/" + att,
			"created_by": "owner", "updated_by": "owner",
		})
		row["text"], row["text_omitted"] = `{"a":1}`, nil
		apiWantBody(t, data, row)
	})

	omitted := []struct {
		name, filename, content, mime, reason string
	}{
		{"a non-text blob is left out as binary", "chart.png", "pretend png", "image/png", "binary"},
		{"a text blob that is not UTF-8 is left out as binary", "notes.txt", "caf\xe9", "text/plain", "binary"},
		{"a text blob over 256 KiB is left out as too_large", "big.txt", strings.Repeat("a", 256<<10+1), "text/plain", "too_large"},
	}
	for _, tc := range omitted {
		t.Run(tc.name, func(t *testing.T) {
			_, h, _, owner := newAPITestServer(t)
			id, att := apiTestSharedDocFile(t, h, owner, "Doc", tc.filename, tc.content)
			status, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			row := apiTestSharedDocRow(map[string]any{
				"id": id, "attachment_id": att, "name": "Doc", "filename": tc.filename,
				"mime": tc.mime, "url": "/api/chat/attachment/" + att,
				"created_by": "owner", "updated_by": "owner",
			})
			row["text"], row["text_omitted"] = nil, tc.reason
			apiWantBody(t, data, row)
		})
	}

	t.Run("a text blob of exactly 256 KiB is still inline", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		content := strings.Repeat("a", 256<<10)
		id, _ := apiTestSharedDocFile(t, h, owner, "Doc", "edge.txt", content)
		_, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if data["text"] != content || data["text_omitted"] != nil {
			t.Fatalf("want the content inline, got text_omitted=%v and %d chars", data["text_omitted"], len(data["text"].(string)))
		}
	})

	t.Run("a link's text is its url and an archived document reports when and by whom", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://example.com/b"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")

		status, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		row := apiTestSharedDocRow(map[string]any{
			"id": id, "kind": "link", "name": "Board", "filename": "", "mime": "text/uri-list",
			"url": "https://example.com/b", "created_by": "owner", "updated_by": "owner",
			"archived_ts": apiAnyNumber, "archived_by": "owner",
		})
		row["text"], row["text_omitted"] = "https://example.com/b", nil
		apiWantBody(t, data, row)
	})

	t.Run("an unknown id answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "GET", "/api/shared-docs/sd-000000000000", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc 'sd-000000000000' not found")
	})
}

func TestHandleReplaceSharedDocApiSharedDocsDocIdReplacePost(t *testing.T) {
	t.Run("new content becomes current under the same id and the replaced version is listed", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id, first := apiTestSharedDocFile(t, h, owner, "Runbook", "runbook.md", "# v1")
		agent := apiTestAgentToken(t, api, "kip", "")
		second := apiTestUploadBlob(t, h, agent, "runbook.md", "# v2")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", agent,
			`{"attachment_id":"`+second+`","description":"now with v2"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 2, nil))
		dashboard.wantFrames(apiTestSharedDocFrame(2, 2, "patch", id, "kip"))

		_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		row := apiTestSharedDocRow(map[string]any{
			"id": id, "attachment_id": second, "description": "now with v2",
			"url": "/api/chat/attachment/" + second, "created_by": "owner",
			"version_count": 2,
		})
		row["text"], row["text_omitted"] = "# v2", nil
		apiWantBody(t, data, row)

		_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id+"/versions", owner, "")
		apiWantBody(t, data, map[string]any{"doc_id": id, "versions": []any{map[string]any{
			"id": 1, "kind": "file", "attachment_id": first, "name": "Runbook", "description": "",
			"filename": "runbook.md", "mime": "text/markdown", "url": "/api/chat/attachment/" + first,
			"created_ts": apiAnyNumber, "created_by": "owner",
		}}})
	})

	t.Run("past the retention depth the count stays at four and the oldest version is gone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		for i, url := range []string{"https://e.com/2", "https://e.com/3", "https://e.com/4", "https://e.com/5"} {
			status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"url":"`+url+`"}`)
			if status != 200 {
				t.Fatalf("replace %d: %d %v", i, status, data)
			}
			want := []int{2, 3, 4, 4}[i]
			apiWantBody(t, data, apiTestSharedDocReceipt(id, want, nil))
		}
		_, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id+"/versions", owner, "")
		version := func(id int, url string) map[string]any {
			return map[string]any{
				"id": id, "kind": "link", "attachment_id": apiAnyString, "name": "Board",
				"description": "", "filename": "", "mime": "text/uri-list", "url": url,
				"created_ts": apiAnyNumber, "created_by": "owner",
			}
		}
		apiWantBody(t, data, map[string]any{"doc_id": id, "versions": []any{
			version(4, "https://e.com/4"), version(3, "https://e.com/3"), version(2, "https://e.com/2"),
		}})
	})

	t.Run("a version dropped past the retention depth takes its file with it unless another doc's version still cites it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		atts := make([]string, 7)
		for i := range atts {
			atts[i] = apiTestUploadBlob(t, h, owner, "runbook.md", "# v"+string(rune('1'+i)))
		}
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"file","name":"Runbook","attachment_id":"`+atts[0]+`"}`)
		copyID := apiTestCreateSharedDoc(t, h, owner, `{"kind":"file","name":"Copy","attachment_id":"`+atts[0]+`"}`)
		if status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+copyID+"/replace", owner,
			`{"attachment_id":"`+atts[6]+`"}`); status != 200 {
			t.Fatalf("replace the copy: %d %v", status, data)
		}
		for _, att := range atts[1:6] {
			if status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner,
				`{"attachment_id":"`+att+`"}`); status != 200 {
				t.Fatalf("replace with %s: %d %v", att, status, data)
			}
		}
		got := map[string]int{}
		for _, att := range atts {
			got[att] = apiRequest(t, h, "GET", "/api/chat/attachment/"+att, owner, "").Code
		}
		want := map[string]int{
			atts[0]: 200, atts[1]: 404, atts[2]: 200, atts[3]: 200, atts[4]: 200, atts[5]: 200,
			atts[6]: 200,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("attachment status by id:\n got %v\nwant %v", got, want)
		}
	})

	t.Run("name and description are kept when omitted, a blank description clears it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner,
			`{"kind":"link","name":"Board","description":"old","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"url":"https://e.com/2"}`)
		_, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if data["name"] != "Board" || data["description"] != "old" {
			t.Fatalf("omitted fields must carry forward, got name=%v description=%v", data["name"], data["description"])
		}
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner,
			`{"url":"https://e.com/3","name":"Roadmap","description":" "}`)
		_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if data["name"] != "Roadmap" || data["description"] != "" {
			t.Fatalf("sent fields must win, got name=%v description=%v", data["name"], data["description"])
		}
	})

	refusals := []struct {
		name, body, code, message string
		status                    int
	}{
		{"a url on a file document answers 400", `{"url":"https://e.com"}`, "validation_error",
			"a file shared doc does not take url: its kind is fixed, so create a new shared doc instead", 400},
		{"no content answers 400", `{}`, "validation_error", "attachment_id is required for a file shared doc", 400},
		{"a blank name answers 400", `{"name":" ","attachment_id":"att-000000000000"}`, "validation_error",
			"name cannot be blank: omit it to keep the current name", 400},
		{"an unknown field answers 422", `{"kind":"link"}`, "validation_error",
			`invalid request body: json: unknown field "kind"`, 422},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			_, h, _, owner := newAPITestServer(t)
			id, _ := apiTestSharedDocFile(t, h, owner, "Runbook", "runbook.md", "# v1")
			status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, tc.body)
			if status != tc.status {
				t.Fatalf("want %d, got %d (%v)", tc.status, status, data)
			}
			apiWantError(t, data, tc.code, tc.message)
			_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id+"/versions", owner, "")
			apiWantBody(t, data, map[string]any{"doc_id": id, "versions": []any{}})
		})
	}

	t.Run("an attachment id on a link document answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"attachment_id":"att-000000000000"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"a link shared doc does not take attachment_id: its kind is fixed, so create a new shared doc instead")
	})

	t.Run("an archived document answers 409 and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		dashboard := apiTestListen(t, api, "")
		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"url":"https://e.com/2"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "shared doc '"+id+"' is archived: unarchive it before changing it")
		dashboard.wantFrames()
	})

	t.Run("an unknown id answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/shared-docs/sd-000000000000/replace", owner, `{"url":"https://e.com"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc 'sd-000000000000' not found")
	})
}

func TestHandleListSharedDocVersionsApiSharedDocsDocIdVersionsGet(t *testing.T) {
	t.Run("an archived document's versions are still listed to a machine", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"url":"https://e.com/2"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/shared-docs/"+id+"/versions", machine, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"doc_id": id, "versions": []any{map[string]any{
			"id": 1, "kind": "link", "attachment_id": apiAnyString, "name": "Board",
			"description": "", "filename": "", "mime": "text/uri-list", "url": "https://e.com/1",
			"created_ts": apiAnyNumber, "created_by": "owner",
		}}})
	})

	t.Run("an unknown id answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "GET", "/api/shared-docs/sd-000000000000/versions", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc 'sd-000000000000' not found")
	})
}

func TestHandleRestoreSharedDocVersionApiSharedDocsDocIdVersionsVersionIdRestorePost(t *testing.T) {
	t.Run("the restored version becomes current, the displaced one takes its place and the count is unchanged", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"url":"https://e.com/2"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/versions/1/restore", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 2, nil))
		dashboard.wantFrames(apiTestSharedDocFrame(3, 3, "patch", id, "kip"))

		_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if data["text"] != "https://e.com/1" || data["updated_by"] != "kip" || data["created_by"] != "owner" {
			t.Fatalf("current after restore: %v", data)
		}
		_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id+"/versions", owner, "")
		apiWantBody(t, data, map[string]any{"doc_id": id, "versions": []any{map[string]any{
			"id": 2, "kind": "link", "attachment_id": apiAnyString, "name": "Board",
			"description": "", "filename": "", "mime": "text/uri-list", "url": "https://e.com/2",
			"created_ts": apiAnyNumber, "created_by": "owner",
		}}})

		status, data = apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/versions/1/restore", agent, "")
		if status != 404 {
			t.Fatalf("a restored version id stops resolving: want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "version 1 is not a retained version of shared doc '"+id+"'")
	})

	t.Run("an archived document answers 409", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", owner, `{"url":"https://e.com/2"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/versions/1/restore", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "shared doc '"+id+"' is archived: unarchive it before changing it")
	})

	t.Run("an unknown document answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/shared-docs/sd-000000000000/versions/1/restore", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc 'sd-000000000000' not found")
	})
}

func TestHandleArchiveSharedDocApiSharedDocsDocIdArchivePost(t *testing.T) {
	t.Run("archiving stamps the first archive; archiving again keeps it and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 1, apiAnyNumber))
		first := data["archived_ts"]
		dashboard.wantFrames(apiTestSharedDocFrame(2, 2, "patch", id, "kip"))

		status, data = apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		if status != 200 || data["archived_ts"] != first {
			t.Fatalf("a repeat archive keeps the first stamp %v: %d %v", first, status, data)
		}
		dashboard.wantFrames()
		_, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if data["archived_by"] != "kip" {
			t.Fatalf("archived_by keeps the first archiver, got %v", data["archived_by"])
		}
	})

	t.Run("an unknown id answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/shared-docs/sd-000000000000/archive", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc 'sd-000000000000' not found")
	})
}

func TestHandleUnarchiveSharedDocApiSharedDocsDocIdUnarchivePost(t *testing.T) {
	t.Run("unarchiving returns the document to the list; unarchiving an active one fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/unarchive", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 1, nil))
		dashboard.wantFrames(apiTestSharedDocFrame(3, 3, "patch", id, "owner"))
		_, data = apiJSON(t, h, "GET", "/api/shared-docs", owner, "")
		if docs, _ := data["docs"].([]any); len(docs) != 1 {
			t.Fatalf("want the document back in the default list, got %v", data)
		}

		status, data = apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/unarchive", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 1, nil))
		dashboard.wantFrames()
	})

	t.Run("a member unarchives a doc the owner archived, and may change it again", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/unarchive", agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 1, nil))
		status, data = apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/replace", agent, `{"url":"https://e.com/2"}`)
		if status != 200 {
			t.Fatalf("replace after unarchive: want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestSharedDocReceipt(id, 2, nil))
	})
}

func TestHandleDeleteSharedDocApiSharedDocsDocIdDelete(t *testing.T) {
	t.Run("the owner deletes an archived document for good and every connection hears the remove", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/shared-docs/"+id, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": id})
		bystander.wantFrames(apiTestSharedDocFrame(3, 3, "remove", id, "owner"))

		status, data = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if status != 404 {
			t.Fatalf("want 404 after delete, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc '"+id+"' not found")
	})

	t.Run("an active document answers 409", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		status, data := apiJSON(t, h, "DELETE", "/api/shared-docs/"+id, owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "shared doc '"+id+"' is not archived: archive it before deleting it")
	})

	t.Run("a member cannot delete, even an archived document", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestCreateSharedDoc(t, h, owner, `{"kind":"link","name":"Board","url":"https://e.com/1"}`)
		apiJSON(t, h, "POST", "/api/shared-docs/"+id+"/archive", owner, "")
		agent := apiTestAgentToken(t, api, "kip", "")
		status, data := apiJSON(t, h, "DELETE", "/api/shared-docs/"+id, agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		status, _ = apiJSON(t, h, "GET", "/api/shared-docs/"+id, owner, "")
		if status != 200 {
			t.Fatalf("the document must survive a refused delete, got %d", status)
		}
	})

	t.Run("an unknown id answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "DELETE", "/api/shared-docs/sd-000000000000", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "shared doc 'sd-000000000000' not found")
	})
}
