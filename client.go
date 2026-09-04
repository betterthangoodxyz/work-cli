package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client is the whole of the CLI's knowledge of the wire: one envelope in,
// one envelope out, errors shaped exactly as docs/api.md promises.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

// apiError is the API's documented error body:
//
//	{ "error": { "code": "forbidden", "message": "…", "details": {…} } }
type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// detailLines renders validation details ("Email can't be blank") one per
// line, for the 422 case where the caller needs to know which field failed.
func (e *apiError) detailLines() []string {
	lines := make([]string, 0, len(e.Details))
	for field, problems := range e.Details {
		for _, problem := range toSlice(problems) {
			lines = append(lines, fmt.Sprintf("  %s: %v", field, problem))
		}
	}
	return lines
}

func toSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{v}
}

func newClient(baseURL, token string) (*client, error) {
	if token == "" {
		return nil, fmt.Errorf("not authenticated — run `work auth login`, or set WORK_TOKEN to a personal access token (Settings → API tokens)")
	}
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// do performs one request and returns the unwrapped `data` payload. A 204
// (delete) yields nil data and no error; any documented error status
// yields an *apiError.
func (c *client) do(method, path string, query url.Values, body any) (json.RawMessage, error) {
	u, err := url.Parse(c.baseURL + "/api/v1" + path)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", c.baseURL, err)
	}
	u.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("could not encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read the response: %w", err)
	}

	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *apiError       `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("the server answered %d with a body that is not the API envelope: %s", resp.StatusCode, truncate(string(payload), 200))
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("the server answered %d without a documented error body: %s", resp.StatusCode, truncate(string(payload), 200))
	}
	return envelope.Data, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
