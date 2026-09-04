package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTableOutputForAList(t *testing.T) {
	data := []byte(`[
		{"id": 1, "name": "Acme", "value_cents": 120050, "created_at": "2026-08-01T10:00:00Z"},
		{"id": 2, "name": "Globex", "value_cents": 9000, "created_at": "2026-08-02T10:00:00Z"}
	]`)

	var b strings.Builder
	if err := printData(&b, data, "table"); err != nil {
		t.Fatal(err)
	}
	out := b.String()

	// Header row, key order preserved from the server (id first).
	if !strings.HasPrefix(out, "id  ") {
		t.Errorf("first column should be id:\n%s", out)
	}
	for _, want := range []string{"name", "value_cents", "created_at", "Acme", "Globex"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	// Cents render as a decimal amount in a table; JSON keeps the contract.
	if !strings.Contains(out, "1200.50") {
		t.Errorf("cents not rendered as an amount:\n%s", out)
	}
}

// Widths are counted in characters, not bytes: padding by byte length
// pushes every cell right of a multi-byte one out of alignment.
func TestTableColumnsAlignWithMultiByteCells(t *testing.T) {
	data := []byte(`[
		{"id": 1, "name": "José Ramírez", "city": "Bogotá"},
		{"id": 2, "name": "Bob Smith", "city": "Denver"}
	]`)

	var b strings.Builder
	if err := printData(&b, data, "table"); err != nil {
		t.Fatal(err)
	}

	// Column positions in characters, which is what a terminal renders.
	startOfLastColumn := func(line string) int {
		fields := strings.Fields(line)
		last := fields[len(fields)-1]
		return utf8.RuneCountInString(line[:strings.LastIndex(line, last)])
	}

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	want := startOfLastColumn(lines[0])
	for _, line := range lines[1:] {
		if got := startOfLastColumn(line); got != want {
			t.Errorf("last column starts at %d, header at %d:\n%s", got, want, b.String())
		}
	}
}

func TestTableOutputForOneRecord(t *testing.T) {
	data := []byte(`{"id": 42, "first_name": "Ada", "email": null}`)

	var b strings.Builder
	if err := printData(&b, data, "table"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "first_name:") || !strings.Contains(out, "Ada") {
		t.Errorf("out = %q", out)
	}
}

func TestTableOutputForAnEmptyList(t *testing.T) {
	var b strings.Builder
	if err := printData(&b, []byte(`[]`), "table"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "no records") {
		t.Errorf("out = %q", b.String())
	}
}

func TestJSONOutputIsPrettyAndExact(t *testing.T) {
	data := []byte(`{"id":42,"value_cents":120050}`)

	var b strings.Builder
	if err := printData(&b, data, "json"); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"id\": 42,\n  \"value_cents\": 120050\n}\n"
	if b.String() != want {
		t.Errorf("out = %q, want %q", b.String(), want)
	}
}

func TestFormatResolution(t *testing.T) {
	cases := []struct {
		format string
		tty    bool
		want   string
	}{
		{"auto", true, "table"},
		{"auto", false, "json"},
		{"json", true, "json"},
		{"table", false, "table"},
	}
	for _, c := range cases {
		g := globalFlags{format: c.format, tty: c.tty}
		if got := g.effectiveFormat(); got != c.want {
			t.Errorf("format=%s tty=%v → %s, want %s", c.format, c.tty, got, c.want)
		}
	}
}

func TestGlobalFlagParsing(t *testing.T) {
	g, rest, err := parseGlobalFlags([]string{"--base-url", "http://localhost:3000", "--format=json", "contacts", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if g.baseURL != "http://localhost:3000" || g.format != "json" {
		t.Errorf("globals = %+v", g)
	}
	if len(rest) != 2 || rest[0] != "contacts" {
		t.Errorf("rest = %v", rest)
	}

	if _, _, err := parseGlobalFlags([]string{"--format", "yaml", "contacts"}); err == nil {
		t.Error("an unknown format should be rejected")
	}
	if _, _, err := parseGlobalFlags([]string{"--nope", "contacts"}); err == nil {
		t.Error("an unknown global flag should be rejected")
	}
}
