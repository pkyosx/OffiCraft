package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var codexModelFamilies = []string{"astra", "sol", "terra", "luna"}

var codexFamilyModelID = regexp.MustCompile(`^gpt-(\d+(?:\.\d+)*)-(astra|sol|terra|luna)$`)

type codexModelEntry struct {
	ID     string
	Hidden bool
}

func isCodexModelFamily(model string) bool {
	for _, family := range codexModelFamilies {
		if model == family {
			return true
		}
	}
	return false
}

// A suffixed variant (`gpt-6-sol-mini`) is a different model, not a newer sol.
func newestCodexFamilyModel(models []codexModelEntry, family string) (string, bool) {
	best, bestVersion := "", []int(nil)
	for _, m := range models {
		if m.Hidden {
			continue
		}
		match := codexFamilyModelID.FindStringSubmatch(m.ID)
		if match == nil || match[2] != family {
			continue
		}
		version := codexModelVersion(match[1])
		if best == "" || compareCodexModelVersions(version, bestVersion) > 0 {
			best, bestVersion = m.ID, version
		}
	}
	return best, best != ""
}

func codexModelVersion(dotted string) []int {
	parts := strings.Split(dotted, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	for len(out) > 1 && out[len(out)-1] == 0 {
		out = out[:len(out)-1]
	}
	return out
}

func compareCodexModelVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

const codexModelListBudget = 15 * time.Second

// listCodexModels asks the Codex App Server itself (`model/list`), not
// ~/.codex/models_cache.json or `codex debug models`: the cache is refreshed by
// whichever codex process happens to run and lags a Codex upgrade, and `debug`
// is not a stable interface. It starts no thread, so it costs no usage.
func listCodexModels(codexBin string) ([]codexModelEntry, error) {
	refuseInTestBinary("listCodexModels(" + codexBin + ")")
	cmd := exec.Command(codexBin, "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	return readCodexModelList(stdin, stdout, codexModelListBudget)
}

func readCodexModelList(stdin io.Writer, stdout io.Reader, budget time.Duration) ([]codexModelEntry, error) {
	messages := codexAppReader(stdout)
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	nextID := 0
	call := func(method string, params map[string]any) (map[string]any, error) {
		nextID++
		id := nextID
		if err := json.NewEncoder(stdin).Encode(appServerMessage{
			"id": id, "method": method, "params": params,
		}); err != nil {
			return nil, err
		}
		for {
			select {
			case <-deadline.C:
				return nil, fmt.Errorf("%s timed out after %s", method, budget)
			case msg, ok := <-messages:
				if !ok {
					return nil, errors.New("app-server exited before answering " + method)
				}
				if messageID(msg) != id {
					continue
				}
				if problem, isErr := msg["error"].(map[string]any); isErr {
					return nil, fmt.Errorf("%s failed: %v", method, problem["message"])
				}
				result, _ := msg["result"].(map[string]any)
				return result, nil
			}
		}
	}
	if _, err := call("initialize", map[string]any{
		"clientInfo": map[string]any{"name": "officraft", "title": "OffiCraft", "version": "0.1.0"},
	}); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(stdin).Encode(appServerMessage{
		"method": "initialized", "params": map[string]any{},
	}); err != nil {
		return nil, err
	}
	var out []codexModelEntry
	params := map[string]any{}
	for page := 0; page < 20; page++ {
		result, err := call("model/list", params)
		if err != nil {
			return nil, err
		}
		data, _ := result["data"].([]any)
		for _, raw := range data {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			hidden, _ := item["hidden"].(bool)
			if id != "" {
				out = append(out, codexModelEntry{ID: id, Hidden: hidden})
			}
		}
		cursor, _ := result["nextCursor"].(string)
		if cursor == "" {
			return out, nil
		}
		params = map[string]any{"cursor": cursor}
	}
	return nil, errors.New("model/list kept paging past 20 pages")
}

func (d SpawnDeps) resolveCodexLaunchModel(model string) (launchModel, refusal string) {
	if !isCodexModelFamily(model) {
		return model, ""
	}
	var models []codexModelEntry
	err := errors.New("this warden was built without a Codex model lister")
	if d.CodexModels != nil {
		models, err = d.CodexModels(d.CodexBin)
	}
	if err != nil {
		d.logf("codex model family: listing models from %s failed: %v", d.CodexBin, err)
		return "", fmt.Sprintf("%s: could not read the model list of this machine's Codex (%s) to pick the newest %s model",
			codexModelFamilyUnavailable, d.codexVersion(), model)
	}
	if resolved, ok := newestCodexFamilyModel(models, model); ok {
		return resolved, ""
	}
	available := make([]string, 0, len(models))
	for _, m := range models {
		if !m.Hidden {
			available = append(available, m.ID)
		}
	}
	return "", fmt.Sprintf("%s: this machine's Codex (%s) lists no %s model; available: %s",
		codexModelFamilyUnavailable, d.codexVersion(), model, strings.Join(available, ", "))
}

// frontend/src/lib/lastOpReason.ts parses both refusal sentences above to
// reword them; change them together or the owner sees the English line.
const codexModelFamilyUnavailable = "codex_model_family_unavailable"

func (d SpawnDeps) codexVersion() string {
	out, err := d.Runner.Run(d.CodexBin, "--version")
	if fields := strings.Fields(out); err == nil && len(fields) > 0 {
		return "version " + fields[len(fields)-1]
	}
	return "version unknown"
}
