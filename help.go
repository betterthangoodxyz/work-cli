package main

import (
	"fmt"
	"io"
	"strings"
)

// printHelp renders the whole command surface from the generated table, so
// help can never describe a command the binary does not have.
func printHelp(w io.Writer) {
	var b strings.Builder
	b.WriteString(`work — the Work CLI. The whole public API, one binary.

Usage:
  work <resource> <verb> [id] [flags]
  work help <resource>       every verb and flag for one resource
  work auth login [--base-url URL]
  work auth logout
  work version

Resources and their verbs:
`)
	width := 0
	for _, spec := range specResources {
		if len(spec.name) > width {
			width = len(spec.name)
		}
	}
	for _, spec := range specResources {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, spec.name, strings.Join(spec.verbs(), ", "))
	}
	b.WriteString(`
A create or update takes the resource's writable fields as flags, e.g.:
  work contacts create --first-name Ada --last-name Lovelace --email ada@example.com
  work invoices create --company-id 7 --issue-on 2026-08-27 --due-on 2026-09-26 \
    --lines '[{"description": "Design retainer", "quantity": "1", "unit_price": "1200.50"}]'

Global flags (before the resource):
  --base-url URL   API host (or WORK_URL; default ` + defaultBaseURL + `)
  --format FORMAT  json | table (default: table on a terminal, JSON when piped)

Auth: a personal access token from Settings → API tokens, in WORK_TOKEN or
saved by ` + "`work auth login`" + `, which reads it from the prompt or from stdin —
never from a flag, so it stays out of your shell history. Tokens are read or
write scoped; writes need a write token. Every response body is the API's
data payload — money fields are integer cents, as in the API (docs/api.md).

Reference: docs/cli.md. Agent skill: cli/SKILL.md.
`)
	io.WriteString(w, b.String())
}

// printResourceHelp shows one resource's whole surface — every verb with
// its usage line and flags — so an agent can discover a resource's writable
// fields without a token and without the web reference.
func printResourceHelp(w io.Writer, spec resourceSpec) {
	var b strings.Builder
	fmt.Fprintf(&b, "work %s — verbs: %s\n\n", spec.name, strings.Join(spec.verbs(), ", "))
	for _, verb := range spec.verbs() {
		printCommandUsage(&b, spec, verb)
	}
	io.WriteString(w, b.String())
}

// printCommandUsage shows one command's flags, from the same table.
func printCommandUsage(w io.Writer, spec resourceSpec, verb string) {
	var b strings.Builder
	switch verb {
	case "list":
		fmt.Fprintf(&b, "Usage: work %s list [--page N] [--per-page N]\n", spec.name)
	case "show", "delete":
		fmt.Fprintf(&b, "Usage: work %s %s <id>\n", spec.name, verb)
	case "create":
		fmt.Fprintf(&b, "Usage: work %s create [flags]\n", spec.name)
		writeFieldFlags(&b, spec.fields)
	case "update":
		fmt.Fprintf(&b, "Usage: work %s update <id> [flags]\n", spec.name)
		writeFieldFlags(&b, spec.fields)
	default:
		if action, ok := spec.findAction(verb); ok {
			fmt.Fprintf(&b, "Usage: work %s %s <id>", spec.name, verb)
			if len(action.fields) > 0 {
				b.WriteString(" [flags]")
			}
			b.WriteByte('\n')
			if action.summary != "" {
				fmt.Fprintf(&b, "  %s\n", action.summary)
			}
			writeFieldFlags(&b, action.fields)
		}
	}
	io.WriteString(w, b.String())
}

func writeFieldFlags(b *strings.Builder, fields []fieldSpec) {
	for _, f := range fields {
		kind := "string"
		switch f.kind {
		case fieldInt:
			kind = "integer"
		case fieldJSON:
			kind = "JSON"
		}
		fmt.Fprintf(b, "  --%-16s %s\n", f.flag+" ", kind)
	}
}
