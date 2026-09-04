package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generated.go is written by bin/docs from docs/openapi.json, and
// bin/docs --check (in bin/ci) fails when the two disagree. These tests are
// the second lock: they re-derive the table from the spec independently, so
// a hand edit to generated.go fails even when nobody ran bin/docs.

// specActionVerbs mirrors CLI_ACTION_VERBS in bin/docs: an action's verb is
// its last path segment, unless a friendlier name is listed there.
var specActionVerbs = map[string]string{"payments": "pay"}

func loadOpenAPISpec(t *testing.T) map[string]any {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("docs", "openapi.json"))
	if err != nil {
		t.Fatalf("cannot read the OpenAPI spec the table was generated from: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(payload, &spec); err != nil {
		t.Fatalf("docs/openapi.json does not parse: %v", err)
	}
	return spec
}

func specPaths(t *testing.T) map[string]map[string]any {
	t.Helper()
	paths := map[string]map[string]any{}
	for path, operations := range loadOpenAPISpec(t)["paths"].(map[string]any) {
		paths[path] = operations.(map[string]any)
	}
	return paths
}

func TestEveryResourceInTheSpecIsACommand(t *testing.T) {
	for path, operations := range specPaths(t) {
		segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(segments) != 1 {
			continue
		}
		spec, ok := findResource(segments[0])
		if !ok {
			t.Errorf("spec path %s has no resource in the command table — regenerate with bin/docs", path)
			continue
		}

		member := specPaths(t)["/"+segments[0]+"/{id}"]
		expectOps(t, segments[0], spec, operations, member)

		if _, creates := operations["post"]; creates {
			wantFields := specBodyFields(t, operations["post"].(map[string]any))
			assertFields(t, segments[0]+" create", spec.fields, wantFields)
		}
	}
}

func expectOps(t *testing.T, name string, spec resourceSpec, collection, member map[string]any) {
	t.Helper()
	var want operation
	if _, ok := collection["get"]; ok {
		want |= opList
	}
	if _, ok := collection["post"]; ok {
		want |= opCreate
	}
	if _, ok := member["get"]; ok {
		want |= opShow
	}
	if _, ok := member["patch"]; ok {
		want |= opUpdate
	}
	if _, ok := member["delete"]; ok {
		want |= opDelete
	}
	if spec.ops != want {
		t.Errorf("%s: ops = %b, want %b from the spec's routes", name, spec.ops, want)
	}
}

func TestEveryActionInTheSpecIsACommand(t *testing.T) {
	for path, operations := range specPaths(t) {
		segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(segments) != 3 {
			continue
		}
		parent, leaf := segments[0], segments[2]
		spec, ok := findResource(parent)
		if !ok {
			t.Errorf("action path %s belongs to %s, which has no command", path, parent)
			continue
		}
		verb := specActionVerbs[leaf]
		if verb == "" {
			verb = leaf
		}
		action, ok := spec.findAction(verb)
		if !ok {
			t.Errorf("spec path %s has no %q action on %s — regenerate with bin/docs", path, verb, parent)
			continue
		}
		if want := fmt.Sprintf("/%s/%%d/%s", parent, leaf); action.path != want {
			t.Errorf("%s %s: path = %q, want %q", parent, verb, action.path, want)
		}
		post, _ := operations["post"].(map[string]any)
		if post == nil {
			t.Errorf("%s is not a POST in the spec", path)
			continue
		}
		assertFields(t, parent+" "+verb, action.fields, specBodyFields(t, post))
		if len(action.fields) > 0 && action.paramKey == "" {
			t.Errorf("%s %s: a body without a paramKey would not be wrapped", parent, verb)
		}
	}
}

// specBodyFields reads a request body's wrapped properties the same way
// bin/docs does: { contact: { first_name: … } } → the inner fields.
func specBodyFields(t *testing.T, operation map[string]any) []fieldSpec {
	t.Helper()
	body, _ := operation["requestBody"].(map[string]any)
	if body == nil {
		return nil
	}
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	if len(properties) != 1 {
		t.Fatalf("a request body should wrap exactly one resource key, got %v", properties)
	}
	for _, wrapped := range properties {
		inner := wrapped.(map[string]any)["properties"].(map[string]any)
		fields := make([]fieldSpec, 0, len(inner))
		for name, property := range inner {
			prop := property.(map[string]any)
			kind := fieldString
			switch prop["type"] {
			case "integer":
				kind = fieldInt
			case "array", "object":
				kind = fieldJSON
			}
			fields = append(fields, fieldSpec{name: name, flag: strings.ReplaceAll(name, "_", "-"), kind: kind})
		}
		return fields
	}
	return nil
}

func assertFields(t *testing.T, context string, got []fieldSpec, want []fieldSpec) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: fields = %v, want %v from the spec", context, fieldNames(got), fieldNames(want))
		return
	}
	// Map iteration order in the spec is not guaranteed; compare as sets.
	have := map[string]fieldSpec{}
	for _, f := range got {
		have[f.name] = f
	}
	for _, w := range want {
		g, ok := have[w.name]
		if !ok {
			t.Errorf("%s: no --%s flag, though the spec's body has %s", context, w.flag, w.name)
			continue
		}
		if g != w {
			t.Errorf("%s: %s = %+v, want %+v", context, w.name, g, w)
		}
	}
}

func fieldNames(fields []fieldSpec) []string {
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.name)
	}
	return names
}

// Two flags with the same name would panic flag.FlagSet at runtime; a field
// whose flag collides (value_cents vs value-cents) is a generation bug.
func TestNoResourceHasDuplicateFlags(t *testing.T) {
	for _, spec := range specResources {
		seen := map[string]bool{}
		for _, f := range spec.fields {
			if seen[f.flag] {
				t.Errorf("%s: --%s is defined twice", spec.name, f.flag)
			}
			seen[f.flag] = true
		}
		if (spec.ops&(opCreate|opUpdate)) != 0 && spec.paramKey == "" {
			t.Errorf("%s: writable but no paramKey to wrap the body in", spec.name)
		}
	}
}
