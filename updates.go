package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The update check is best-effort and interactive-only: it runs after a
// successful resource command, only at a terminal, at most once per day,
// and every failure is silent. It never delays or fails the command it
// follows, never touches config.json (which holds the credential), and
// never sends the token anywhere — the release host is not the API.

const defaultReleasesURL = "https://github.com/betterthangoodxyz/work-cli/releases"

// updateCheckState is update-check.json, next to config.json: the latest
// version seen and when the release host was last asked.
type updateCheckState struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

func updateCheckPath() string {
	return filepath.Join(filepath.Dir(configPath()), "update-check.json")
}

// maybeNotifyUpdate refreshes the cached latest version when it is a day
// stale, then prints a one-line notice to stderr when this binary is
// behind. Called only after a successful command at a TTY, so the notice
// can never be mistaken for output or an error by a pipe or an agent.
func maybeNotifyUpdate(stderr io.Writer) {
	if os.Getenv("WORK_NO_UPDATE_CHECK") != "" {
		return
	}
	current, ok := parseVersion(version)
	if !ok {
		return // a dev build has nothing meaningful to compare
	}

	state := loadUpdateCheckState()
	if time.Since(state.CheckedAt) >= 24*time.Hour {
		// Record the attempt before its outcome, so a dead network costs
		// one try per day, not one per command.
		state.CheckedAt = time.Now()
		if latest, err := fetchLatestVersion(); err == nil {
			state.Latest = latest
		}
		saveUpdateCheckState(state)
	}

	latest, ok := parseVersion(state.Latest)
	if ok && versionLess(current, latest) {
		fmt.Fprintf(stderr, "work: %s is available (you have %s) — update: curl -fsSL %s/install-cli | bash\n",
			state.Latest, version, defaultBaseURL)
	}
}

// fetchLatestVersion asks the release host which tag "latest" points at,
// reading the redirect's Location rather than any API: one unauthenticated
// HEAD, no JSON schema to depend on, no rate limit to worry about at once
// a day. WORK_RELEASES_BASE overrides the host, as it does for install-cli.
func fetchLatestVersion() (string, error) {
	base := os.Getenv("WORK_RELEASES_BASE")
	if base == "" {
		base = defaultReleasesURL
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Head(strings.TrimRight(base, "/") + "/latest")
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	location := resp.Header.Get("Location")
	tag := location[strings.LastIndex(location, "/")+1:]
	if _, ok := parseVersion(tag); !ok {
		return "", fmt.Errorf("no version tag in redirect to %q", location)
	}
	return tag, nil
}

func loadUpdateCheckState() updateCheckState {
	payload, err := os.ReadFile(updateCheckPath())
	if err != nil {
		return updateCheckState{}
	}
	var state updateCheckState
	if err := json.Unmarshal(payload, &state); err != nil {
		return updateCheckState{}
	}
	return state
}

// saveUpdateCheckState writes via a temp file and rename, so a concurrent
// invocation reads either state whole, never an interleaving. Failures are
// silent: an unwritable home just means checking again next time.
func saveUpdateCheckState(state updateCheckState) {
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	path := updateCheckPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "update-check-*")
	if err != nil {
		return
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
	}
}

// parseVersion reads "1.2.3" or "v1.2.3"; anything else — "dev", an empty
// cache — is not comparable and answers false.
func parseVersion(s string) ([3]int, bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var v [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

func versionLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
