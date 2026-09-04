package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeReleases serves the one thing the check reads: a redirect from
// /latest to the tag of the newest release, exactly as the release host
// answers. It records whether it was asked and what credentials came along.
func fakeReleases(t *testing.T, tag string) (*httptest.Server, *int, *string) {
	t.Helper()
	hits := 0
	auth := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		auth = r.Header.Get("Authorization")
		w.Header().Set("Location", "/releases/tag/"+tag)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)
	return server, &hits, &auth
}

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestUpdateCheckNotifiesWhenBehind(t *testing.T) {
	server, hits, auth := fakeReleases(t, "v9.9.9")
	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	t.Setenv("WORK_RELEASES_BASE", server.URL+"/releases")
	setVersion(t, "1.0.2")

	var stderr bytes.Buffer
	maybeNotifyUpdate(&stderr)

	if *hits != 1 {
		t.Fatalf("release host asked %d times, want 1", *hits)
	}
	if *auth != "" {
		t.Errorf("the check sent credentials: %q", *auth)
	}
	if !strings.Contains(stderr.String(), "v9.9.9 is available") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "install-cli") {
		t.Errorf("notice does not say how to update: %q", stderr.String())
	}

	// Within the day, the cache answers — no second request.
	stderr.Reset()
	maybeNotifyUpdate(&stderr)
	if *hits != 1 {
		t.Errorf("release host asked again within a day: %d hits", *hits)
	}
	if !strings.Contains(stderr.String(), "v9.9.9 is available") {
		t.Errorf("cached notice missing: %q", stderr.String())
	}
}

func TestUpdateCheckSilentWhenCurrentOrAhead(t *testing.T) {
	for _, installed := range []string{"9.9.9", "10.0.0"} {
		server, _, _ := fakeReleases(t, "v9.9.9")
		t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
		t.Setenv("WORK_RELEASES_BASE", server.URL+"/releases")
		setVersion(t, installed)

		var stderr bytes.Buffer
		maybeNotifyUpdate(&stderr)
		if stderr.Len() != 0 {
			t.Errorf("version %s against v9.9.9: stderr = %q", installed, stderr.String())
		}
	}
}

func TestUpdateCheckSkipsDevBuildsAndOptOut(t *testing.T) {
	server, hits, _ := fakeReleases(t, "v9.9.9")
	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	t.Setenv("WORK_RELEASES_BASE", server.URL+"/releases")

	setVersion(t, "dev")
	var stderr bytes.Buffer
	maybeNotifyUpdate(&stderr)
	if *hits != 0 || stderr.Len() != 0 {
		t.Errorf("dev build: %d hits, stderr %q", *hits, stderr.String())
	}

	setVersion(t, "1.0.0")
	t.Setenv("WORK_NO_UPDATE_CHECK", "1")
	maybeNotifyUpdate(&stderr)
	if *hits != 0 || stderr.Len() != 0 {
		t.Errorf("opted out: %d hits, stderr %q", *hits, stderr.String())
	}
}

// A dead release host costs one attempt per day, and never a word: the
// failure is recorded exactly like a success would be.
func TestUpdateCheckFailureIsSilentAndDaily(t *testing.T) {
	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	t.Setenv("WORK_RELEASES_BASE", "http://127.0.0.1:1/releases")
	setVersion(t, "1.0.0")

	var stderr bytes.Buffer
	maybeNotifyUpdate(&stderr)
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
	state := loadUpdateCheckState()
	if state.CheckedAt.IsZero() {
		t.Error("the failed attempt was not recorded, so every command would retry")
	}
}

// The check must never mistake the API's redirect answer for a version, or
// notify from a cache entry that does not parse.
func TestUpdateCheckIgnoresAnUnparseableTag(t *testing.T) {
	server, _, _ := fakeReleases(t, "latest-and-greatest")
	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	t.Setenv("WORK_RELEASES_BASE", server.URL+"/releases")
	setVersion(t, "1.0.0")

	var stderr bytes.Buffer
	maybeNotifyUpdate(&stderr)
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// A stale cache refreshes after a day, and a corrupt state file is a fresh
// start, not a crash.
func TestUpdateCheckRefreshesAfterADay(t *testing.T) {
	server, hits, _ := fakeReleases(t, "v9.9.9")
	dir := t.TempDir()
	t.Setenv("WORK_CONFIG", dir+"/config.json")
	t.Setenv("WORK_RELEASES_BASE", server.URL+"/releases")
	setVersion(t, "1.0.0")

	stale, _ := json.Marshal(updateCheckState{Latest: "v1.0.1", CheckedAt: time.Now().Add(-25 * time.Hour)})
	if err := os.WriteFile(dir+"/update-check.json", stale, 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	maybeNotifyUpdate(&stderr)
	if *hits != 1 {
		t.Errorf("stale cache not refreshed: %d hits", *hits)
	}
	if !strings.Contains(stderr.String(), "v9.9.9 is available") {
		t.Errorf("stderr = %q", stderr.String())
	}

	if err := os.WriteFile(dir+"/update-check.json", []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	maybeNotifyUpdate(&stderr) // must not panic; refetches
	if *hits != 2 {
		t.Errorf("corrupt state not treated as empty: %d hits", *hits)
	}
}

// The notice reaches a person at a terminal after a successful command, and
// never a pipe — an agent parsing stderr for `code: message` must not see it.
func TestUpdateNoticeOnlyAtATTY(t *testing.T) {
	releases, _, _ := fakeReleases(t, "v9.9.9")
	t.Setenv("WORK_RELEASES_BASE", releases.URL+"/releases")
	setVersion(t, "1.0.0")

	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, []any{})
	})

	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	_, _, stderr := runCLI(t, api, "", true, "contacts", "list")
	if !strings.Contains(stderr, "v9.9.9 is available") {
		t.Errorf("tty stderr = %q", stderr)
	}

	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	_, _, stderr = runCLI(t, api, "", false, "contacts", "list")
	if strings.Contains(stderr, "available") {
		t.Errorf("piped stderr carries the notice: %q", stderr)
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.1", "1.0.0", false},
		{"1.9.0", "1.10.0", true},
		{"1.0.0", "2.0.0", true},
		{"2.0.0", "2.0.0", false},
	}
	for _, c := range cases {
		av, _ := parseVersion(c.a)
		bv, _ := parseVersion(c.b)
		if got := versionLess(av, bv); got != c.less {
			t.Errorf("versionLess(%s, %s) = %v", c.a, c.b, got)
		}
	}
	for _, bad := range []string{"dev", "", "1.0", "1.0.0.0", "1.x.0", "-1.0.0"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("parseVersion(%q) parsed", bad)
		}
	}
}
