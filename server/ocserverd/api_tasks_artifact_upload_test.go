package main

import (
	"strings"
	"testing"
)

func TestHandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPost(t *testing.T) {
	t.Run("the raw body is stored and pinned in one call, and reads back on the task's artifact set", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&description=what+it+says&filename=report.md&mime=text/markdown",
			agent, "# the report\n")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 1,
		})
		artifactID, _ := data["artifact_id"].(string)

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{map[string]any{
			"id":            artifactID,
			"kind":          "file",
			"attachment_id": apiAnyString,
			"name":          "the report",
			"description":   "what it says",
			"filename":      "report.md",
			"mime":          "text/markdown",
			"url":           apiAnyString,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
			"version_count": 1,
		}})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an image mime pins the deliverable as an image, because the bytes decide the kind", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=chart&filename=chart.png&mime=image/png",
			agent, "pretend png bytes")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		artifactID, _ := data["artifact_id"].(string)

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{map[string]any{
			"id":            artifactID,
			"kind":          "image",
			"attachment_id": apiAnyString,
			"name":          "chart",
			"description":   "",
			"filename":      "chart.png",
			"mime":          "image/png",
			"url":           apiAnyString,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
			"version_count": 1,
		}})
	})

	t.Run("a request with no ?name= at all is refused by the query binder and pins nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload", agent, "bytes")
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "Query argument name is required, but not found")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})

	t.Run("a blank name answers 400 asking for a display name and pins nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=+++", agent, "bytes")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"name is required: give this deliverable a short display name")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})

	t.Run("a name or description over its cap answers 400 naming both numbers and pins nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name="+strings.Repeat("x", 49), agent, "bytes")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "artifact name is 49 chars, over the 48-char limit")

		status, data = apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=fine&description="+strings.Repeat("y", 257),
			agent, "bytes")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact description is 257 chars, over the 256-char limit")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
	})

	t.Run("a request with no bytes at all answers 400 and pins nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=empty", agent, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment is empty")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=late", agent, "bytes")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403 and pins nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=mine", other, "bytes")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=mine", machine, "bytes")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/artifacts/upload?name=x", owner, "bytes")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and pins nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifacts/upload?name=mine", "", "bytes")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, pinned := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", pinned["artifacts"], []any{})
		dashboard.wantFrames()
	})
}

func TestHandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost(t *testing.T) {
	t.Run("new bytes take over the same artifact id, and the name and description carry forward", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&description=what+it+says&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			agent, "# v2\n")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  2,
		})

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		apiWantValue(t, "artifacts", listed["artifacts"], []any{map[string]any{
			"id":            artifactID,
			"kind":          "file",
			"attachment_id": apiAnyString,
			"name":          "the report",
			"description":   "what it says",
			"filename":      "report-v2.md",
			"mime":          "text/markdown",
			"url":           apiAnyString,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
			"version_count": 2,
		}})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("a name and a description sent alongside the bytes replace the stored ones", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&description=what+it+says&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?name=the+final+report&description=&filename=report-v2.md&mime=text/markdown",
			agent, "# v2\n")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.version_count", data["version_count"], 2)

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.name", row["name"], "the final report")
		apiWantValue(t, "artifact.description", row["description"], "")
	})

	t.Run("bytes of the other kind answer 400 naming both kinds and the stored content stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=chart&filename=chart.png&mime=image/png",
			agent, "pretend png bytes")
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=chart.txt&mime=text/plain",
			agent, "not a chart any more")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a image and the "+
				"replacement asks for a file — un-pin it and register a new artifact instead")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.kind", row["kind"], "image")
		apiWantValue(t, "artifact.filename", row["filename"], "chart.png")
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})

	t.Run("a link artifact refuses a byte replacement, because a link's content is a url", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=notes.md&mime=text/markdown",
			agent, "# notes\n")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a link and the "+
				"replacement asks for a file — un-pin it and register a new artifact instead")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.kind", row["kind"], "link")
		apiWantValue(t, "artifact.url", row["url"], "https://example.com/pr/123")
		dashboard.wantFrames()
	})

	t.Run("a blank ?name= answers 400 pointing at omission and the stored name stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?name=+++&filename=report-v2.md&mime=text/markdown",
			agent, "# v2\n")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"name cannot be blank: omit it to keep the name this deliverable already has")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.name", row["name"], "the report")
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})

	t.Run("a request with no bytes at all answers 400 and the stored content stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			agent, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment is empty")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.filename", row["filename"], "report.md")
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})

	t.Run("an artifact id this task does not carry answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/ta-nosuchartifact/replace/upload?filename=x.md&mime=text/markdown",
			agent, "# x\n")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosuchartifact' not found")
		dashboard.wantFrames()
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			agent, "# v2\n")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.filename", row["filename"], "report.md")
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403 and the stored content stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		mira := apiTestAgentToken(t, api, "mira", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			mira, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			other, "# v2\n")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.filename", row["filename"], "report.md")
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		machine := apiTestAgentToken(t, api, "m-server-self", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			machine, "# v2\n")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})

	t.Run("an id no task carries answers 404 naming the task", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-999/artifact/ta-nosuchartifact/replace/upload?filename=x.md&mime=text/markdown",
			owner, "# x\n")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401 and the stored content stands", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifacts/upload?name=the+report&filename=report.md&mime=text/markdown",
			agent, "# v1\n")
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST",
			"/api/tasks/T-1/artifact/"+artifactID+"/replace/upload?filename=report-v2.md&mime=text/markdown",
			"", "# v2\n")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")

		_, listed := apiJSON(t, h, "GET", "/api/tasks/T-1/artifacts", owner, "")
		artifacts, _ := listed["artifacts"].([]any)
		row, _ := artifacts[0].(map[string]any)
		apiWantValue(t, "artifact.filename", row["filename"], "report.md")
		apiWantValue(t, "artifact.version_count", row["version_count"], 1)
		dashboard.wantFrames()
	})
}

func TestReadArtifactUploadBody(t *testing.T) {
	t.Skip("pinned end to end, EXCEPT the 100 MB ceiling: the happy paths assert " +
		"the filename and mime it resolves onto the stored blob, and both " +
		"routes assert the empty-body 400. The over-cap refusal is left " +
		"unpinned deliberately — producing it means pushing a >100 MB body " +
		"through the in-memory stack.")
}

func TestArtifactKindOfBlob(t *testing.T) {
	t.Skip("pinned end to end: \"an image mime pins the deliverable as an image…\" " +
		"and \"bytes of the other kind answer 400 naming both kinds…\" assert " +
		"both verdicts this read reaches, through the artifact rows they " +
		"produce.")
}
