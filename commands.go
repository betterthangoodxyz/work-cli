package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// fieldKind is how a writable API field's flag value is read: plain string,
// integer (ids, cents), or a JSON document (an invoice's lines).
type fieldKind int

const (
	fieldString fieldKind = iota
	fieldInt
	fieldJSON
)

// fieldSpec is one writable field: its API name, its CLI flag, and how to
// parse the flag's value back into the type the API expects.
type fieldSpec struct {
	name string
	flag string
	kind fieldKind
}

// operation is a bitmask of the verbs a routed resource answers to. The
// server draws exactly these routes (config/routes.rb), so a verb absent
// here — update on an invoice, create on a payment — is absent there too.
type operation uint

const (
	opList operation = 1 << iota
	opShow
	opCreate
	opUpdate
	opDelete
)

// actionSpec is a designed action rather than a column write — send this
// invoice, record a payment against it (docs/api.md). path carries one %d
// for the record's id.
type actionSpec struct {
	verb     string
	path     string
	summary  string
	paramKey string
	fields   []fieldSpec
}

// resourceSpec is one API resource's whole command surface.
type resourceSpec struct {
	name     string
	singular string
	paramKey string
	ops      operation
	fields   []fieldSpec
	actions  []actionSpec
}

func findResource(name string) (resourceSpec, bool) {
	for _, spec := range specResources {
		if spec.name == name || spec.singular == name {
			return spec, true
		}
	}
	return resourceSpec{}, false
}

// supports answers whether this resource actually has the verb — the same
// question runResource's switch asks, so the two cannot disagree.
func (r resourceSpec) supports(verb string) bool {
	for _, candidate := range r.verbs() {
		if candidate == verb {
			return true
		}
	}
	return false
}

func (r resourceSpec) findAction(verb string) (actionSpec, bool) {
	for _, action := range r.actions {
		if action.verb == verb {
			return action, true
		}
	}
	return actionSpec{}, false
}

// verbs lists what the resource answers to, in the order help shows them.
func (r resourceSpec) verbs() []string {
	verbs := []string{}
	for _, candidate := range []struct {
		op   operation
		verb string
	}{
		{opList, "list"}, {opShow, "show"}, {opCreate, "create"},
		{opUpdate, "update"}, {opDelete, "delete"},
	} {
		if r.ops&candidate.op != 0 {
			verbs = append(verbs, candidate.verb)
		}
	}
	for _, action := range r.actions {
		verbs = append(verbs, action.verb)
	}
	return verbs
}

// ui carries the streams and the output format so tests can capture exactly
// what a user or an agent would see.
type ui struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	tty    bool
	format string // "json" or "table", already resolved from --format/auto
}

func runResource(c *client, spec resourceSpec, args []string, u ui) int {
	if len(args) == 0 {
		u.usage("%s needs a verb — one of: %s", spec.name, strings.Join(spec.verbs(), ", "))
		return 2
	}
	verb, rest := args[0], args[1:]

	var (
		data json.RawMessage
		err  error
	)

	switch verb {
	case "list":
		if spec.ops&opList == 0 {
			return u.unsupportedVerb(spec, verb)
		}
		data, err = u.runList(c, spec, rest)
	case "show":
		if spec.ops&opShow == 0 {
			return u.unsupportedVerb(spec, verb)
		}
		data, err = u.runShow(c, spec, rest)
	case "create", "update":
		op := opCreate
		if verb == "update" {
			op = opUpdate
		}
		if spec.ops&op == 0 {
			return u.unsupportedVerb(spec, verb)
		}
		data, err = u.runWrite(c, spec, verb, rest)
	case "delete":
		if spec.ops&opDelete == 0 {
			return u.unsupportedVerb(spec, verb)
		}
		err = u.runDelete(c, spec, rest)
	default:
		action, ok := spec.findAction(verb)
		if !ok {
			return u.unsupportedVerb(spec, verb)
		}
		data, err = u.runAction(c, spec, action, rest)
	}

	if err != nil {
		var misuse *usageError
		if errors.As(err, &misuse) {
			fmt.Fprintf(u.stderr, "work: %v.\n", misuse)
			return 2
		}
		u.printError(err)
		return 1
	}
	if data != nil {
		if err := printData(u.stdout, data, u.format); err != nil {
			fmt.Fprintf(u.stderr, "work: %v\n", err)
			return 1
		}
	}
	return 0
}

// usageError is a caller mistake — bad flag, missing id, a verb the API
// does not have. It exits 2 so a script can tell "you asked for something
// that does not exist" apart from "the server refused".
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func (u ui) usage(format string, args ...any) int {
	fmt.Fprintf(u.stderr, "work: "+format+".\n", args...)
	return 2
}

func (u ui) unsupportedVerb(spec resourceSpec, verb string) int {
	return u.usage("%s has no %q in the API — its verbs are: %s", spec.name, verb, strings.Join(spec.verbs(), ", "))
}

func (u ui) printError(err error) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		fmt.Fprintf(u.stderr, "work: %v\n", apiErr)
		for _, line := range apiErr.detailLines() {
			fmt.Fprintln(u.stderr, line)
		}
		return
	}
	fmt.Fprintf(u.stderr, "work: %v\n", err)
}

func (u ui) runList(c *client, spec resourceSpec, args []string) (json.RawMessage, error) {
	fs := newFlagSet(spec.name + " list")
	page := fs.Int("page", 0, "page of results (default 1)")
	perPage := fs.Int("per-page", 0, "records per page (default 25, maximum 100)")
	if err := fs.Parse(args); err != nil {
		return nil, classifyFlagError(err)
	}
	if fs.NArg() != 0 {
		return nil, &usageError{"list takes no arguments, only --page and --per-page"}
	}

	query := url.Values{}
	if *page > 0 {
		query.Set("page", strconv.Itoa(*page))
	}
	if *perPage > 0 {
		query.Set("per_page", strconv.Itoa(*perPage))
	}
	return c.do("GET", "/"+spec.name, query, nil)
}

func (u ui) runShow(c *client, spec resourceSpec, args []string) (json.RawMessage, error) {
	id, err := singleID(spec, "show", args)
	if err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, &usageError{fmt.Sprintf("unexpected argument %q — show takes only an id", args[1])}
	}
	return c.do("GET", fmt.Sprintf("/%s/%d", spec.name, id), nil, nil)
}

// runWrite covers create and update: the writable fields become flags, and
// the set ones become the wrapped body the API expects ({contact: {…}}).
// The grammar is `work <resource> <verb> [id] [--flags]` — the id leads, so
// flag parsing (which stops at the first positional argument) sees flags
// only.
func (u ui) runWrite(c *client, spec resourceSpec, verb string, args []string) (json.RawMessage, error) {
	var id int
	if verb == "update" {
		var err error
		id, err = singleID(spec, verb, args)
		if err != nil {
			return nil, err
		}
		args = args[1:]
	}

	fs := newFlagSet(spec.name + " " + verb)
	values := bindFieldFlags(fs, spec.fields)
	if err := fs.Parse(args); err != nil {
		return nil, classifyFlagError(err)
	}
	if fs.NArg() != 0 {
		return nil, &usageError{fmt.Sprintf("unexpected argument %q — %s %s takes an id then --flags", fs.Arg(0), spec.name, verb)}
	}

	fields, err := setFields(fs, spec.fields, values)
	if err != nil {
		return nil, err
	}
	if verb == "update" && len(fields) == 0 {
		return nil, &usageError{"nothing to update — pass at least one --flag"}
	}

	body := map[string]any{spec.paramKey: fields}
	if verb == "create" {
		return c.do("POST", "/"+spec.name, nil, body)
	}
	return c.do("PATCH", fmt.Sprintf("/%s/%d", spec.name, id), nil, body)
}

func (u ui) runDelete(c *client, spec resourceSpec, args []string) error {
	id, err := singleID(spec, "delete", args)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return &usageError{fmt.Sprintf("unexpected argument %q — delete takes only an id", args[1])}
	}
	// The API answers 204 with no body; there is deliberately nothing to
	// print — the exit status is the answer a script needs.
	_, err = c.do("DELETE", fmt.Sprintf("/%s/%d", spec.name, id), nil, nil)
	return err
}

func (u ui) runAction(c *client, spec resourceSpec, action actionSpec, args []string) (json.RawMessage, error) {
	id, err := singleID(spec, action.verb, args)
	if err != nil {
		return nil, err
	}

	fs := newFlagSet(spec.name + " " + action.verb)
	values := bindFieldFlags(fs, action.fields)
	if err := fs.Parse(args[1:]); err != nil {
		return nil, classifyFlagError(err)
	}
	if fs.NArg() != 0 {
		return nil, &usageError{fmt.Sprintf("unexpected argument %q — %s %s takes an id then --flags", fs.Arg(0), spec.name, action.verb)}
	}

	var body any
	if len(action.fields) > 0 {
		fields, err := setFields(fs, action.fields, values)
		if err != nil {
			return nil, err
		}
		body = map[string]any{action.paramKey: fields}
	}
	return c.do("POST", fmt.Sprintf(action.path, id), nil, body)
}

// singleID reads the leading id argument; anything after it is flags for
// the caller's parser to judge.
func singleID(spec resourceSpec, verb string, args []string) (int, error) {
	if len(args) == 0 {
		return 0, &usageError{fmt.Sprintf("%s %s needs an id", spec.name, verb)}
	}
	id, err := strconv.Atoi(args[0])
	if err != nil || id < 1 {
		return 0, &usageError{fmt.Sprintf("%q is not an id — expected a positive integer", args[0])}
	}
	return id, nil
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// classifyFlagError wraps a flag mistake as misuse. -h/--help never reaches
// a parser: run intercepts it before a client exists (help needs no token),
// and past `--` the flag package treats it as a positional argument.
func classifyFlagError(err error) error {
	return &usageError{err.Error()}
}

// bindFieldFlags registers one string flag per writable field. Everything is
// a string at the flag line; setFields converts to the field's real type,
// which is what lets --lines take a JSON document and --company-id a number.
func bindFieldFlags(fs *flag.FlagSet, fields []fieldSpec) map[string]*string {
	values := make(map[string]*string, len(fields))
	for _, f := range fields {
		values[f.flag] = fs.String(f.flag, "", flagUsage(f))
	}
	return values
}

func flagUsage(f fieldSpec) string {
	switch f.kind {
	case fieldInt:
		return "integer"
	case fieldJSON:
		return "JSON value"
	default:
		return ""
	}
}

// setFields collects only the flags the caller actually set, converting each
// to its API type — so an unset field is absent from the body rather than
// sent as a zero value the server would have to distinguish from intent.
func setFields(fs *flag.FlagSet, fields []fieldSpec, values map[string]*string) (map[string]any, error) {
	set := map[string]any{}
	var err error
	fs.Visit(func(fl *flag.Flag) {
		if err != nil {
			return
		}
		f := fieldByFlag(fields, fl.Name)
		raw := *values[fl.Name]
		switch f.kind {
		case fieldInt:
			n, convErr := strconv.Atoi(raw)
			if convErr != nil {
				err = &usageError{fmt.Sprintf("--%s expects an integer, got %q", fl.Name, raw)}
				return
			}
			set[f.name] = n
		case fieldJSON:
			var v any
			if convErr := json.Unmarshal([]byte(raw), &v); convErr != nil {
				err = &usageError{fmt.Sprintf("--%s expects a JSON value, got %q", fl.Name, truncate(raw, 60))}
				return
			}
			set[f.name] = v
		default:
			set[f.name] = raw
		}
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}

func fieldByFlag(fields []fieldSpec, flagName string) fieldSpec {
	for _, f := range fields {
		if f.flag == flagName {
			return f
		}
	}
	return fieldSpec{name: flagName, flag: flagName, kind: fieldString}
}
