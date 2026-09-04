package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// fakeAPI records the last request and answers from a scripted handler, so a
// test asserts exactly what the CLI sent — method, path, query, body — not a
// re-implementation of the server.
type fakeAPI struct {
	t       *testing.T
	server  *httptest.Server
	handler http.HandlerFunc

	lastMethod  string
	lastPath    string
	lastQuery   url.Values
	lastBody    map[string]any
	lastRawBody []byte
	lastAuth    string
}

func newFakeAPI(t *testing.T, handler http.HandlerFunc) *fakeAPI {
	f := &fakeAPI{t: t, handler: handler}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastMethod = r.Method
		f.lastPath = r.URL.Path
		f.lastQuery = r.URL.Query()
		f.lastAuth = r.Header.Get("Authorization")
		f.lastRawBody, _ = io.ReadAll(r.Body)
		f.lastBody = nil
		if len(f.lastRawBody) > 0 {
			var body map[string]any
			if err := json.Unmarshal(f.lastRawBody, &body); err != nil {
				t.Errorf("the CLI sent a body that is not JSON: %v", err)
			}
			f.lastBody = body
		}
		handler(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// respondData answers the API envelope for a record or list.
func respondData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func respondError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"code": code, "message": message}
	if details != nil {
		body["details"] = details
	}
	json.NewEncoder(w).Encode(map[string]any{"error": body})
}

// runCLI executes the binary's entry point against the fake server, with the
// token supplied via the environment the same way an agent would set it.
func runCLI(t *testing.T, api *fakeAPI, stdin string, tty bool, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("WORK_TOKEN", "work_pat_test")
	if os.Getenv("WORK_CONFIG") == "" {
		t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")
	}
	if api != nil {
		args = append([]string{"--base-url", api.server.URL}, args...)
	}
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(stdin), &stdout, &stderr, tty)
	return code, stdout.String(), stderr.String()
}

func TestListPassesPaginationAndUnwrapsData(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, []any{map[string]any{"id": 1, "first_name": "Ada"}})
	})

	code, stdout, _ := runCLI(t, api, "", false, "contacts", "list", "--page", "2", "--per-page", "50")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if api.lastMethod != "GET" || api.lastPath != "/api/v1/contacts" {
		t.Errorf("got %s %s", api.lastMethod, api.lastPath)
	}
	if got := api.lastQuery.Get("page"); got != "2" {
		t.Errorf("page = %q", got)
	}
	if got := api.lastQuery.Get("per_page"); got != "50" {
		t.Errorf("per_page = %q", got)
	}
	if api.lastAuth != "Bearer work_pat_test" {
		t.Errorf("Authorization = %q", api.lastAuth)
	}
	// Piped output is the data payload as JSON — what an agent parses.
	if !strings.Contains(stdout, `"first_name": "Ada"`) || strings.Contains(stdout, `"data"`) {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestCreateWrapsOnlyTheSetFlags(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 201, map[string]any{"id": 42})
	})

	code, _, _ := runCLI(t, api, "", false,
		"contacts", "create", "--first-name", "Ada", "--company-id", "7")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if api.lastMethod != "POST" || api.lastPath != "/api/v1/contacts" {
		t.Errorf("got %s %s", api.lastMethod, api.lastPath)
	}
	want := map[string]any{"contact": map[string]any{"first_name": "Ada", "company_id": float64(7)}}
	if !jsonEqual(api.lastBody, want) {
		t.Errorf("body = %v, want %v", api.lastBody, want)
	}
}

func TestInvoiceCreateTakesLinesAsJSON(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 201, map[string]any{"id": 88, "status": "draft"})
	})

	code, _, _ := runCLI(t, api, "", false,
		"invoices", "create", "--company-id", "7", "--issue-on", "2026-08-27", "--due-on", "2026-09-26",
		"--lines", `[{"description": "Design retainer", "quantity": "1", "unit_price": "1200.50"}]`)

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	invoice, ok := api.lastBody["invoice"].(map[string]any)
	if !ok {
		t.Fatalf("body = %v", api.lastBody)
	}
	lines, ok := invoice["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("lines = %v", invoice["lines"])
	}
	line := lines[0].(map[string]any)
	if line["unit_price"] != "1200.50" {
		t.Errorf("unit_price = %v", line["unit_price"])
	}
}

func TestUpdateNeedsAnIDAndAField(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, map[string]any{"id": 12, "stage": "won"})
	})

	code, _, _ := runCLI(t, api, "", false, "deals", "update", "412", "--stage", "won")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if api.lastMethod != "PATCH" || api.lastPath != "/api/v1/deals/412" {
		t.Errorf("got %s %s", api.lastMethod, api.lastPath)
	}
	if want := map[string]any{"deal": map[string]any{"stage": "won"}}; !jsonEqual(api.lastBody, want) {
		t.Errorf("body = %v, want %v", api.lastBody, want)
	}

	code, _, stderr := runCLI(t, api, "", false, "deals", "update", "412")
	if code != 2 || !strings.Contains(stderr, "nothing to update") {
		t.Errorf("update without flags: exit %d, stderr %q", code, stderr)
	}
}

func TestDeleteIsSilentOnSuccess(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	code, stdout, _ := runCLI(t, api, "", false, "contacts", "delete", "12")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if api.lastMethod != "DELETE" || api.lastPath != "/api/v1/contacts/12" {
		t.Errorf("got %s %s", api.lastMethod, api.lastPath)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (the API answers 204)", stdout)
	}
}

func TestInvoiceActions(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondData(w, 200, map[string]any{"id": 88, "status": "sent"})
	})

	code, _, _ := runCLI(t, api, "", false, "invoices", "send", "88")
	if code != 0 {
		t.Fatalf("send: exit %d", code)
	}
	if api.lastMethod != "POST" || api.lastPath != "/api/v1/invoices/88/send" {
		t.Errorf("send: got %s %s", api.lastMethod, api.lastPath)
	}
	if len(api.lastRawBody) != 0 {
		t.Errorf("send: body = %q, want none", api.lastRawBody)
	}

	code, _, _ = runCLI(t, api, "", false,
		"invoices", "pay", "88", "--amount", "1200.50", "--paid-on", "2026-08-27")
	if code != 0 {
		t.Fatalf("pay: exit %d", code)
	}
	if api.lastPath != "/api/v1/invoices/88/payments" {
		t.Errorf("pay: got %s", api.lastPath)
	}
	want := map[string]any{"payment": map[string]any{"amount": "1200.50", "paid_on": "2026-08-27"}}
	if !jsonEqual(api.lastBody, want) {
		t.Errorf("pay: body = %v, want %v", api.lastBody, want)
	}
}

func TestReadOnlyResourceRefusesWritesBeforeAnyRequest(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {})

	code, _, stderr := runCLI(t, api, "", false, "payments", "create")

	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, `no "create"`) {
		t.Errorf("stderr = %q", stderr)
	}
	if api.lastMethod != "" {
		t.Errorf("a request was made: %s %s", api.lastMethod, api.lastPath)
	}
}

func TestAPIErrorsSurfaceCodeMessageAndDetails(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		respondError(w, 422, "unprocessable_entity", "Validation failed.",
			map[string]any{"email": []any{"can't be blank"}})
	})

	code, _, stderr := runCLI(t, api, "", false, "contacts", "create", "--first-name", "Ada")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "unprocessable_entity: Validation failed.") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "email: can't be blank") {
		t.Errorf("stderr missing details: %q", stderr)
	}
}

func TestUnknownResourceAndVerbAreMisuse(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {})

	code, _, stderr := runCLI(t, api, "", false, "widgets", "list")
	if code != 2 || !strings.Contains(stderr, `unknown resource "widgets"`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}

	code, _, stderr = runCLI(t, api, "", false, "contacts", "frobnicate")
	if code != 2 || !strings.Contains(stderr, `no "frobnicate"`) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestMissingTokenIsAClearError(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {})
	t.Setenv("WORK_TOKEN", "")
	t.Setenv("WORK_CONFIG", t.TempDir()+"/config.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--base-url", api.server.URL, "contacts", "list"},
		strings.NewReader(""), &stdout, &stderr, false)

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not authenticated") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestHelpListsEveryResourceFromTheTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"help"}, strings.NewReader(""), &stdout, &stderr, false)

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, spec := range specResources {
		if !strings.Contains(stdout.String(), spec.name) {
			t.Errorf("help does not mention %s", spec.name)
		}
	}
	if !strings.Contains(stdout.String(), "WORK_TOKEN") {
		t.Errorf("help does not explain auth: %q", stdout.String())
	}
}

// -h must reach usage on every verb. show and delete hand their arguments
// straight to singleID, which would otherwise reject "-h" as not an id.
func TestHelpFlagOnEveryVerb(t *testing.T) {
	cases := []struct{ args []string }{
		{[]string{"contacts", "list", "-h"}},
		{[]string{"contacts", "show", "-h"}},
		{[]string{"contacts", "create", "--help"}},
		{[]string{"contacts", "update", "12", "-h"}},
		{[]string{"contacts", "delete", "-h"}},
		{[]string{"invoices", "send", "-h"}},
		{[]string{"invoices", "pay", "88", "-h"}},
	}
	for _, c := range cases {
		code, stdout, stderr := runCLI(t, nil, "", false, c.args...)
		if code != 0 {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, stderr)
		}
		if !strings.HasPrefix(stdout, "Usage: work ") {
			t.Errorf("%v: stdout = %q", c.args, stdout)
		}
	}

	// A verb the resource does not have is still a refusal, not usage for
	// an endpoint that isn't there.
	code, stdout, _ := runCLI(t, nil, "", false, "payments", "create", "-h")
	if code != 2 || stdout != "" {
		t.Errorf("payments create -h: exit %d, stdout %q", code, stdout)
	}
}

// Help must never require a credential — an agent's first move on a new
// machine is discovery, before any token exists. Guards the regression
// where `work contacts --help` answered "not authenticated".
func TestHelpNeedsNoToken(t *testing.T) {
	t.Setenv("WORK_CONFIG", t.TempDir()+"/absent.json")

	for _, args := range [][]string{
		{"contacts", "--help"},
		{"help", "contacts"},
		{"deals", "create", "-h"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(args, strings.NewReader(""), &stdout, &stderr, false)
		if code != 0 {
			t.Errorf("%v without a token: exit %d, stderr %q", args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage: work ") {
			t.Errorf("%v: stdout = %q", args, stdout.String())
		}
	}
}

// `work help <resource>` and `work <resource> --help` list every verb with
// its flags, so a resource's writable fields are discoverable from the
// binary alone.
func TestResourceHelpListsEveryVerbAndFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"help", "deals"}, strings.NewReader(""), &stdout, &stderr, false)

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	spec, _ := findResource("deals")
	for _, verb := range spec.verbs() {
		if !strings.Contains(stdout.String(), "work deals "+verb) {
			t.Errorf("resource help misses verb %q: %q", verb, stdout.String())
		}
	}
	for _, f := range spec.fields {
		if !strings.Contains(stdout.String(), "--"+f.flag) {
			t.Errorf("resource help misses flag --%s", f.flag)
		}
	}

	code, _, stderr2 := runCLI(t, nil, "", false, "help", "widgets")
	if code != 2 || !strings.Contains(stderr2, "unknown resource") {
		t.Errorf("help widgets: exit %d, stderr %q", code, stderr2)
	}
}

// A bare resource is a misuse answered locally — naming the verbs, sending
// nothing, needing no token.
func TestBareResourceNamesItsVerbs(t *testing.T) {
	t.Setenv("WORK_CONFIG", t.TempDir()+"/absent.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{"contacts"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "needs a verb") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 || !strings.Contains(stdout.String(), "work ") {
		t.Errorf("exit %d, stdout %q", code, stdout.String())
	}
}

// jsonEqual compares decoded JSON by re-encoding, so map ordering and
// numeric float64-ness don't produce false mismatches.
func jsonEqual(got, want map[string]any) bool {
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	return bytes.Equal(g, w)
}
