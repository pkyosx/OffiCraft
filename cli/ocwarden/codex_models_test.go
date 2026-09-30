package main

import (
	"encoding/json"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeCodexAppServer struct {
	mu       sync.Mutex
	requests []string
}

func (f *fakeCodexAppServer) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func startFakeCodexAppServer(t *testing.T,
	reply func(id any, method string, params map[string]any) (messages []appServerMessage, exit bool),
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
		enc := json.NewEncoder(stdoutW)
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
			id, isRequest := msg["id"]
			if !isRequest {
				continue
			}
			messages, exit := reply(id, method, params)
			if exit {
				_ = stdoutW.Close()
				return
			}
			for _, m := range messages {
				_ = enc.Encode(m)
			}
		}
	}()
	return stdinW, stdoutR, fake
}

func codexResult(id any, result map[string]any) []appServerMessage {
	return []appServerMessage{{"id": id, "result": result}}
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

func codexModelPage(nextCursor any, models ...map[string]any) map[string]any {
	data := make([]any, 0, len(models))
	for _, m := range models {
		data = append(data, m)
	}
	return map[string]any{"data": data, "nextCursor": nextCursor}
}

func TestReadCodexModelList(t *testing.T) {
	t.Run("models on every page are read until a null nextCursor", func(t *testing.T) {
		stdin, stdout, fake := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			switch {
			case method == "initialize":
				return codexResult(id, map[string]any{}), false
			case method == "model/list" && params["cursor"] == nil:
				return codexResult(id, codexModelPage("page-2",
					map[string]any{"id": "gpt-6-sol", "hidden": false},
					map[string]any{"id": "gpt-6-luna"})), false
			case method == "model/list" && params["cursor"] == "page-2":
				return codexResult(id, codexModelPage(nil,
					map[string]any{"id": "gpt-6.1-sol", "hidden": false})), false
			}
			return nil, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 2*time.Second)

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
	})

	t.Run("a model listed as hidden is read as hidden", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			switch method {
			case "initialize":
				return codexResult(id, map[string]any{}), false
			case "model/list":
				return codexResult(id, map[string]any{"data": []any{
					map[string]any{"id": "gpt-7-sol", "hidden": true},
					map[string]any{"id": "gpt-6-sol", "hidden": false},
				}}), false
			}
			return nil, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 2*time.Second)

		if err != nil {
			t.Fatalf("readCodexModelList: %v", err)
		}
		wantModels := []codexModelEntry{{ID: "gpt-7-sol", Hidden: true}, {ID: "gpt-6-sol"}}
		if !reflect.DeepEqual(models, wantModels) {
			t.Errorf("models = %#v, want %#v", models, wantModels)
		}
	})

	t.Run("a reply to another id, a notification and a model without an id are not read into the list", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			switch method {
			case "initialize":
				return codexResult(id, map[string]any{}), false
			case "model/list":
				return []appServerMessage{
					{"id": 99, "result": codexModelPage(nil, map[string]any{"id": "gpt-9-sol"})},
					{"method": "model/rerouted", "params": map[string]any{"data": []any{}}},
					{"id": id, "result": codexModelPage(nil,
						map[string]any{"id": ""},
						map[string]any{"hidden": false},
						map[string]any{"id": "gpt-6-sol"})},
				}, false
			}
			return nil, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 2*time.Second)

		if err != nil {
			t.Fatalf("readCodexModelList: %v", err)
		}
		wantModels := []codexModelEntry{{ID: "gpt-6-sol"}}
		if !reflect.DeepEqual(models, wantModels) {
			t.Errorf("models = %#v, want %#v", models, wantModels)
		}
	})

	t.Run("an error reply to model/list is returned as that method's failure", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			if method == "initialize" {
				return codexResult(id, map[string]any{}), false
			}
			return []appServerMessage{{"id": id, "error": map[string]any{"code": -32603, "message": "not signed in"}}}, false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 2*time.Second)

		if models != nil {
			t.Errorf("models = %#v, want nil", models)
		}
		if err == nil || err.Error() != "model/list failed: not signed in" {
			t.Errorf("err = %v, want model/list failed: not signed in", err)
		}
	})

	t.Run("an app server that never answers model/list times out", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			if method == "initialize" {
				return codexResult(id, map[string]any{}), false
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

	t.Run("pages that each answer within the budget but together exceed it time out", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			if method == "initialize" {
				return codexResult(id, map[string]any{}), false
			}
			time.Sleep(100 * time.Millisecond)
			switch params["cursor"] {
			case nil:
				return codexResult(id, codexModelPage("page-2", map[string]any{"id": "gpt-6-sol"})), false
			case "page-2":
				return codexResult(id, codexModelPage("page-3", map[string]any{"id": "gpt-6-luna"})), false
			}
			return codexResult(id, codexModelPage(nil, map[string]any{"id": "gpt-6-terra"})), false
		})

		models, err := readCodexModelListWithin(t, stdin, stdout, 150*time.Millisecond)

		if models != nil {
			t.Errorf("models = %#v, want nil", models)
		}
		if err == nil || err.Error() != "model/list timed out after 150ms" {
			t.Errorf("err = %v, want model/list timed out after 150ms", err)
		}
	})

	t.Run("an app server that exits before answering model/list is reported as exited", func(t *testing.T) {
		stdin, stdout, _ := startFakeCodexAppServer(t, func(id any, method string, params map[string]any) ([]appServerMessage, bool) {
			if method == "initialize" {
				return codexResult(id, map[string]any{}), false
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
