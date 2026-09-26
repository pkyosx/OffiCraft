package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const userAgent = "ocagent/0.1"

const httpTimeout = 10 * time.Second

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

func defaultHTTPClient() *http.Client { return &http.Client{Timeout: httpTimeout} }

func httpRequest(client httpClient, method, url, token string, jsonBody any) (int, string) {
	var body io.Reader
	if jsonBody != nil {
		raw, err := json.Marshal(jsonBody)
		if err != nil {
			return 0, err.Error()
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func getJSON(client httpClient, cfg Config, path string, authed bool) (int, any) {
	token := ""
	if authed {
		token = cfg.Token
	}
	status, text := httpRequest(client, http.MethodGet, cfg.Base+path, token, nil)
	return status, safeJSON(text)
}

func postJSON(client httpClient, cfg Config, path string, payload any) (int, any) {
	status, text := httpRequest(client, http.MethodPost, cfg.Base+path, cfg.Token, payload)
	return status, safeJSON(text)
}

func safeJSON(text string) any {
	var obj any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil
	}
	return obj
}
