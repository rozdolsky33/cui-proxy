package clientprobe

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestShapeRecordsStructureNotContent(t *testing.T) {
	body := `{"model":"m-1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"private words here"}]}],
		"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"secret_field":{"type":"string"}}}}],
		"stream":true,"n":3,"x":null}`
	got, err := Shape([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"$: object",
		"input: array",
		"input[]: object",
		"input[].content: array",
		"input[].content[]: object",
		"input[].content[].text: string",
		"input[].content[].type: string",
		"input[].content[].type=input_text",
		"input[].role: string",
		"input[].role=user",
		"input[].type: string",
		"input[].type=message",
		"model: string",
		"n: number",
		"stream: bool",
		"tools: array",
		"tools[]: object",
		"tools[].name: string",
		"tools[].parameters: object",
		"tools[].type: string",
		"tools[].type=function",
		"x: null",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shape mismatch:\n got %q\nwant %q", got, want)
	}
	for _, e := range got {
		if strings.Contains(e, "private") || strings.Contains(e, "secret_field") || strings.Contains(e, "m-1") || strings.Contains(e, "shell") {
			t.Fatalf("entry carries content: %q", e)
		}
	}
}

func TestShapeTreatsNestedToolSchemasAsOpaque(t *testing.T) {
	got, err := Shape([]byte(`{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"n","tools":[
		{"type":"custom","name":"exec","format":{"type":"grammar","definition":"d"}},
		{"type":"function","name":"f","parameters":{"properties":{"p":{"type":"string"}}}}]}]}],"client_metadata":{"k":"v"}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if strings.Contains(e, "properties") || strings.Contains(e, "definition") || strings.Contains(e, "client_metadata.") {
			t.Errorf("descended into an opaque value: %q", e)
		}
	}
	for _, want := range []string{"input[].tools[].tools[].type=custom", "input[].tools[].type=namespace", "input[].tools[].tools[].format: object"} {
		found := false
		for _, e := range got {
			found = found || e == want
		}
		if !found {
			t.Errorf("missing %q", want)
		}
	}
}

func TestShapeHidesNonIdentifierDiscriminators(t *testing.T) {
	got, err := Shape([]byte(`{"type":"two words"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"$: object", "type: string", "type=<other>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestShapeRejectsInvalidJSON(t *testing.T) {
	for _, in := range []string{``, `{`, `{} {}`} {
		if _, err := Shape([]byte(in)); err == nil {
			t.Errorf("Shape(%q) succeeded", in)
		}
	}
}

func TestDiffAndMerge(t *testing.T) {
	base := []string{"a", "b"}
	next := Merge([]string{"b"}, []string{"c", "b"})
	added, removed := Diff(base, next)
	if !reflect.DeepEqual(added, []string{"c"}) || !reflect.DeepEqual(removed, []string{"a"}) {
		t.Fatalf("added %q removed %q", added, removed)
	}
}
