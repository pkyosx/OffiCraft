package main

import (
	"encoding/json"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNewestCodexFamilyModel(t *testing.T) {
	list := []codexModelEntry{
		{ID: "gpt-5.6-sol"},
		{ID: "gpt-6-sol"},
		{ID: "gpt-6.1-sol"},
		{ID: "gpt-7-sol", Hidden: true},
		{ID: "gpt-6.2-sol-mini"},
		{ID: "gpt-6-luna"},
		{ID: "gpt-5.6-luna"},
		{ID: "gpt-5.6-terra"},
		{ID: "gpt-10-astra"},
		{ID: "gpt-9.9-astra"},
		{ID: "gpt-5.5"},
		{ID: "codex-auto-review", Hidden: true},
	}
	cases := []struct {
		family string
		want   string
		found  bool
	}{
		{"sol", "gpt-6.1-sol", true},
		{"luna", "gpt-6-luna", true},
		{"terra", "gpt-5.6-terra", true},
		{"astra", "gpt-10-astra", true},
		{"gpt-5.5", "", false},
	}
	for _, c := range cases {
		got, found := newestCodexFamilyModel(list, c.family)
		if got != c.want || found != c.found {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.family, got, found, c.want, c.found)
		}
	}

	if got, found := newestCodexFamilyModel([]codexModelEntry{{ID: "gpt-6-terra", Hidden: true}}, "terra"); found {
		t.Errorf("a family whose only model is hidden resolved to %q", got)
	}
	if got, _ := newestCodexFamilyModel([]codexModelEntry{{ID: "gpt-6-sol"}, {ID: "gpt-6.0-sol"}}, "sol"); got != "gpt-6-sol" {
		t.Errorf("two spellings of one version: got %q, want the first listed, gpt-6-sol", got)
	}
}

type fakeCodexAppServer struct {
	mu       sync.Mutex
	requests []string
}

func (f *fakeCodexAppServer) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// startFakeCodexAppServer answers each JSON-RPC request with reply's result; a nil
// result leaves the request unanswered and exit=true closes stdout like a dead process.
func startFakeCodexAppServer(t *testing.T,
	reply func(method string, params map[string]any) (result map[string]any, exit bool),
) (io.Writer, io.Reader, *fakeCodexAppServer) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinW.Close()
		_ = stdoutW.Close()
	})
	fake := &fakeCodexAppServer{}
	go func() {
		dec := json.NewDecoder(stdinR)
		for {
			var msg appServerMessage
			if dec.Decode(&msg) != nil {
				return
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			raw, _ := json.Marshal(params)
			fake.mu.Lock()
			fake.requests = append(fake.requests, method+" "+string(raw))
			fake.mu.Unlock()
			if _, isRequest := msg["id"]; !isRequest {
				continue
			}
			result, exit := reply(method, params)
			if exit {
				_ = stdoutW.Close()
				return
			}
			if result == nil {
				continue
			}
			_ = json.NewEncoder(stdoutW).Encode(appServerMessage{"id": msg["id"], "result": result})
		}
	}()
	return stdinW, stdoutR, fake
}

func readCodexModelListWithin(t *testing.T, stdin io.Writer, stdout io.Reader,
	budget time.Duration) ([]codexModelEntry, error) {
	type outcome struct {
		models []codexModelEntry
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		models, err := readCodexModelList(stdin, stdout, budget)
		done <- outcome{models, err}
	}()
	select {
	case o := <-done:
		return o.models, o.err
	case <-time.After(5 * time.Second):
		t.Fatal("readCodexModelList never returned")
		return nil, nil
	}
}

func codexModelPage(nextCursor string, models ...map[string]any) map[string]any {
	data := make([]any, 0, len(models))
	for _, m := range models {
		data = append(data, m)
	}
	page := map[string]any{"data": data}
	if nextCursor != "" {
		page["nextCursor"] = nextCursor
	}
	return page
}

func TestReadCodexModelList(t *testing.T) {
	t.Run("models on every page are read and the newest sol is on the second page", func(t *testing.T) {
		stdin, stdout, fake := startFakeCodexAppServer(t, func(method string, params map[string]any) (map[string]any, bool) {
			switch {
			case method == "initialize":
				return map[string]any{}, false
			case method == "model/list" && params["cursor"] == nil:
				return codexModelPage("page-2",
					map[string]any{"id": "gpt-6-sol", "hidden": false},
					map[string]any{"id": "gpt-6-luna"}), false
			case method == "model/list" && params["cursor"] == "page-2":
				return codexModelPage("", map[string]any{"id": "gpt-6.1-sol", "hidden": false}), false
			}
			return nil, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 5*time.Second)

		if err != nil {
			t.Fatalf("readCodexModelList: %v", err)
		}
		wantModels := []codexModelEntry{{ID: "gpt-6-sol"}, {ID: "gpt-6-luna"}, {ID: "gpt-6.1-sol"}}
		if !reflect.DeepEqual(models, wantModels) {
			t.Errorf("models = %#v, want %#v", models, wantModels)
		}
		wantRequests := []string{
			`initialize {"clientInfo":{"name":"officraft","title":"OffiCraft","version":"0.1.0"}}`,
			`initialized {}`,
			`model/list {}`,
			`model/list {"cursor":"page-2"}`,
		}
		if got := fake.seen(); !reflect.DeepEqual(got, wantRequests) {
			t.Errorf("requests = %q, want %q", got, wantRequests)
		}
		if got, _ := newestCodexFamilyModel(models, "sol"); got != "gpt-6.1-sol" {
			t.Errorf("newest sol = %q, want gpt-6.1-sol", got)
		}
	})

	t.Run("a model listed as hidden is read as hidden and is not picked", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(method string, params map[string]any) (map[string]any, bool) {
			switch method {
			case "initialize":
				return map[string]any{}, false
			case "model/list":
				return codexModelPage("",
					map[string]any{"id": "gpt-7-sol", "hidden": true},
					map[string]any{"id": "gpt-6-sol", "hidden": false}), false
			}
			return nil, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 5*time.Second)

		if err != nil {
			t.Fatalf("readCodexModelList: %v", err)
		}
		wantModels := []codexModelEntry{{ID: "gpt-7-sol", Hidden: true}, {ID: "gpt-6-sol"}}
		if !reflect.DeepEqual(models, wantModels) {
			t.Errorf("models = %#v, want %#v", models, wantModels)
		}
		if got, _ := newestCodexFamilyModel(models, "sol"); got != "gpt-6-sol" {
			t.Errorf("newest sol = %q, want gpt-6-sol", got)
		}
	})

	t.Run("an app server that never answers model/list times out", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(method string, params map[string]any) (map[string]any, bool) {
			if method == "initialize" {
				return map[string]any{}, false
			}
			return nil, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 100*time.Millisecond)

		if models != nil {
			t.Errorf("models = %#v, want nil", models)
		}
		if err == nil || err.Error() != "model/list timed out after 100ms" {
			t.Errorf("err = %v, want model/list timed out after 100ms", err)
		}
	})

	t.Run("an app server that exits before answering model/list is reported as exited", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(method string, params map[string]any) (map[string]any, bool) {
			if method == "initialize" {
				return map[string]any{}, false
			}
			return nil, true
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 2*time.Second)

		if models != nil {
			t.Errorf("models = %#v, want nil", models)
		}
		if err == nil || err.Error() != "app-server exited before answering model/list" {
			t.Errorf("err = %v, want app-server exited before answering model/list", err)
		}
	})
}
