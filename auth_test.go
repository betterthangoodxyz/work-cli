package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthLoginVerifiesThenSavesTheToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WORK_CONFIG", configPath)

	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer work_pat_good" {
			respondError(w, 401, "unauthorized", "Invalid or missing access token.", nil)
			return
		}
		respondData(w, 200, []any{})
	})

	code, stdout, _ := runCLI(t, api, "work_pat_good\n", false, "auth", "login")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "Authenticated") {
		t.Errorf("stdout = %q", stdout)
	}

	payload, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config not saved: %v", err)
	}
	var saved fileConfig
	if err := json.Unmarshal(payload, &saved); err != nil {
		t.Fatalf("saved config is not JSON: %v", err)
	}
	if saved.Token != "work_pat_good" {
		t.Errorf("saved token = %q", saved.Token)
	}
	info, _ := os.Stat(configPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600 — the file holds a live credential", info.Mode().Perm())
	}

	// A bad token fails at login, and nothing is saved.
	os.Remove(configPath)
	code, _, stderr := runCLI(t, api, "work_pat_bad\n", false, "auth", "login")
	if code != 1 || !strings.Contains(stderr, "unauthorized") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("a refused token must not be saved")
	}
}

func TestAuthLoginCanSaveACustomBaseURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WORK_CONFIG", configPath)

	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, []any{})
	})

	code, _, _ := runCLI(t, api, "work_pat_x\n", false, "auth", "login", "--base-url", api.server.URL)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := loadConfig().BaseURL; got != api.server.URL {
		t.Errorf("saved base URL = %q", got)
	}

	// Rotating the token must not move the CLI back to the default host:
	// the next command would carry the new credential to a server it was
	// never issued for.
	code, _, _ = runCLI(t, api, "work_pat_rotated\n", false, "auth", "login")
	if code != 0 {
		t.Fatalf("rotation exit %d", code)
	}
	saved := loadConfig()
	if saved.Token != "work_pat_rotated" {
		t.Errorf("token = %q, want the rotated one", saved.Token)
	}
	if saved.BaseURL != api.server.URL {
		t.Errorf("base URL = %q after rotating the token, want it kept", saved.BaseURL)
	}
}

// A token in argv lands in shell history and every process list, so the
// flag does not exist — the CLI must refuse it rather than quietly reading
// the token some other way.
func TestAuthLoginRejectsATokenFlag(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, []any{})
	})

	code, _, stderr := runCLI(t, api, "", false, "auth", "login", "--token", "work_pat_x")

	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "flag provided but not defined") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAuthLogoutRemovesTheConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WORK_CONFIG", configPath)
	if err := saveConfig(fileConfig{Token: "work_pat_x"}); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, nil, "", false, "auth", "logout")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config still present after logout")
	}
	if !strings.Contains(stdout, "Logged out") {
		t.Errorf("stdout = %q", stdout)
	}

	// Logging out twice is not an error.
	code, _, _ = runCLI(t, nil, "", false, "auth", "logout")
	if code != 0 {
		t.Errorf("second logout: exit %d", code)
	}
}

func TestConfigIsReadWhenTheEnvHasNoToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WORK_CONFIG", configPath)
	t.Setenv("WORK_TOKEN", "")
	if err := saveConfig(fileConfig{Token: "work_pat_saved"}); err != nil {
		t.Fatal(err)
	}

	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, []any{})
	})

	var stdout, stderr strings.Builder
	code := run([]string{"--base-url", api.server.URL, "contacts", "list"},
		strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if api.lastAuth != "Bearer work_pat_saved" {
		t.Errorf("Authorization = %q, want the saved token", api.lastAuth)
	}
}
