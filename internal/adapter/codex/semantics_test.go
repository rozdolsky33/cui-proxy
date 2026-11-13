package codex

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cui-proxy/internal/sse"
)

// These contracts fail if request scanning is skipped, mappings churn between
// turns, or restored tool arguments alter the effects of a local command.
func semanticSecret() string { return "4" + strings.Repeat("1", 15) }

func semanticJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal("marshal failed")
	}
	return b
}

func semanticRequest(t *testing.T, e env, input any) []byte {
	t.Helper()
	out := mustSanitize(t, e, semanticJSON(t, map[string]any{"model": "test-model", "input": input}))
	if bytes.Contains(out, []byte(semanticSecret())) {
		t.Fatal("original crossed request boundary")
	}
	return out
}

func semanticMessage(text string) map[string]any {
	return map[string]any{"role": "user", "content": text}
}

func semanticArguments(t *testing.T, e env, arguments string, width int) string {
	t.Helper()
	rw := New().NewResponseRewriter(e.r)
	var events []sse.Event
	runes := []rune(arguments)
	for start := 0; start < len(runes); start += width {
		end := min(start+width, len(runes))
		data := semanticJSON(t, map[string]any{"type": "response.function_call_arguments.delta", "item_id": "call-a", "output_index": 0, "delta": string(runes[start:end])})
		events = append(events, ev("response.function_call_arguments.delta", string(data)))
	}
	events = append(events, ev("response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","item_id":"call-a","output_index":0}`))
	return deltas(t, feed(t, rw, events), "response.function_call_arguments.delta")
}

func TestSemanticsRepeatedHistory(t *testing.T) {
	e := newEnv(t)
	var history []any
	var previous string
	for turn := 0; turn < 64; turn++ {
		history = append(history, semanticMessage("remember "+semanticSecret()))
		out := semanticRequest(t, e, history)
		var doc struct{ Input []struct{ Content string } }
		if json.Unmarshal(out, &doc) != nil {
			t.Fatal("decode failed")
		}
		if len(doc.Input) != turn+1 {
			t.Fatal("history lost")
		}
		for _, item := range doc.Input {
			if previous != "" && item.Content != previous {
				t.Fatal("mapping changed across history")
			}
			if e.r.Text(item.Content) != "remember "+semanticSecret() {
				t.Fatal("history restoration failed")
			}
		}
		previous = doc.Input[0].Content
		// Re-sanitizing a retained upstream representation must not remap it.
		if twice := mustSanitize(t, e, out); !bytes.Equal(twice, out) {
			t.Fatal("sanitized history is not stable")
		}
	}
}

func TestSemanticsCompactionDelegation(t *testing.T) {
	e := newEnv(t)
	const cipher = "Y29tcGFjdGVkLXN0YXRl"
	out := semanticRequest(t, e, []any{
		map[string]any{"type": "compaction", "encrypted_content": cipher, "summary": "remember " + semanticSecret()},
		map[string]any{"type": "function_call_output", "call_id": "delegate-a", "output": "delegated context " + semanticSecret()},
		map[string]any{"type": "custom_tool_call_output", "call_id": "delegate-b", "output": "delegated context " + semanticSecret()},
		semanticMessage("continue " + semanticSecret()),
	})
	var doc struct{ Input []map[string]any }
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal("decode failed")
	}
	if doc.Input[0]["encrypted_content"] != cipher {
		t.Fatal("ciphertext modified")
	}
	if e.r.Text(doc.Input[0]["summary"].(string)) != "remember "+semanticSecret() {
		t.Fatal("summary not restored")
	}
	for _, item := range doc.Input[1:3] {
		if e.r.Text(item["output"].(string)) != "delegated context "+semanticSecret() {
			t.Fatal("delegated context not restored")
		}
	}
}

func TestSemanticsReconnect(t *testing.T) {
	e := newEnv(t)
	out := semanticRequest(t, e, []any{semanticMessage(semanticSecret())})
	var doc struct{ Input []struct{ Content string } }
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal("decode failed")
	}
	synthetic := doc.Input[0].Content
	old := New().NewResponseRewriter(e.r)
	prefix := string(semanticJSON(t, map[string]any{"type": "response.output_text.delta", "item_id": "same", "delta": synthetic[:len(synthetic)/2]}))
	if _, err := old.Event(ev("response.output_text.delta", prefix)); err != nil {
		t.Fatal("interrupted stream failed")
	}
	// A new response has fresh buffering even when an item ID is reused.
	fresh := New().NewResponseRewriter(e.r)
	data := string(semanticJSON(t, map[string]any{"type": "response.output_text.delta", "item_id": "same", "delta": "resumed " + synthetic}))
	got := deltas(t, feed(t, fresh, []sse.Event{ev("response.output_text.delta", data)}), "response.output_text.delta")
	if got != "resumed "+semanticSecret() {
		t.Fatal("response state crossed reconnect boundary")
	}
	// The old response must not affect subsequent full-history requests either.
	if again := semanticRequest(t, e, []any{semanticMessage(semanticSecret())}); !bytes.Equal(again, out) {
		t.Fatal("mapping lost at reconnect")
	}
}

func TestSemanticsCommandEffects(t *testing.T) {
	e := newEnv(t)
	// Fixed scripts only; never execute provider-generated code. The argument
	// containing quotes, a backslash and Unicode must remain a single argument.
	text := semanticSecret() + " 'quoted' \\ snowman ☃"
	config := `{"account":"` + semanticSecret() + `","retries":7,"ratio":1.25,"scale":1e+3,"large":9007199254740993}`
	args := string(semanticJSON(t, map[string]any{"argv": []string{"/bin/sh", "-c", `printf '%s' "$1" > result.txt; printf '%s' "$2" > config.json`, "semantic-test", text, config}}))
	out := semanticRequest(t, e, []any{map[string]any{"type": "function_call", "name": "exec_command", "call_id": "call-a", "arguments": args}})
	var doc struct{ Input []struct{ Arguments string } }
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal("decode failed")
	}
	for _, width := range []int{1, 7, 4096} {
		restored := semanticArguments(t, e, doc.Input[0].Arguments, width)
		var call struct{ Argv []string }
		if json.Unmarshal([]byte(restored), &call) != nil {
			t.Fatal("arguments invalid")
		}
		if len(call.Argv) != 6 || call.Argv[0] != "/bin/sh" {
			t.Fatal("argv shape changed")
		}
		dir := t.TempDir()
		cmd := exec.Command(call.Argv[0], call.Argv[1:]...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatal("fixed command failed")
		}
		content, err := os.ReadFile(filepath.Join(dir, "result.txt"))
		if err != nil || string(content) != text {
			t.Fatal("command changed file content or quoting")
		}
		content, err = os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			t.Fatal("configuration missing")
		}
		dec := json.NewDecoder(bytes.NewReader(content))
		dec.UseNumber()
		var values map[string]any
		if dec.Decode(&values) != nil {
			t.Fatal("configuration invalid")
		}
		if values["account"] != semanticSecret() || values["retries"] != json.Number("7") || values["ratio"] != json.Number("1.25") || values["scale"] != json.Number("1e+3") || values["large"] != json.Number("9007199254740993") {
			t.Fatal("configuration semantics changed")
		}
	}
}

func TestSemanticsStreamVariants(t *testing.T) {
	for _, kind := range []string{"response.output_text.delta", "response.refusal.delta", "response.reasoning_summary_text.delta", "response.custom_tool_call_input.delta"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			out := semanticRequest(t, e, []any{semanticMessage(semanticSecret())})
			var doc struct{ Input []struct{ Content string } }
			if json.Unmarshal(out, &doc) != nil {
				t.Fatal("decode failed")
			}
			syn := doc.Input[0].Content
			for _, terminal := range []string{"response.completed", "response.failed", "response.incomplete", "error"} {
				var events []sse.Event
				for _, part := range []string{syn[:len(syn)/2], syn[len(syn)/2:]} {
					events = append(events, ev(kind, string(semanticJSON(t, map[string]any{"type": kind, "item_id": "item-a", "delta": part}))))
				}
				events = append(events, ev(terminal, string(semanticJSON(t, map[string]any{"type": terminal}))))
				if got := deltas(t, feed(t, New().NewResponseRewriter(e.r), events), kind); got != semanticSecret() {
					t.Fatal("stream variant lost restored content")
				}
			}
		})
	}
}

// A restored custom-tool patch must change the intended file, not merely be
// valid text. Paths and command are fixed; this never executes model code.
func TestSemanticsPatchEffects(t *testing.T) {
	e := newEnv(t)
	desired := "account=" + semanticSecret() + "\n"
	patch := "diff --git a/settings.txt b/settings.txt\n--- a/settings.txt\n+++ b/settings.txt\n@@ -1 +1 @@\n-before\n+" + desired
	out := semanticRequest(t, e, []any{map[string]any{"type": "custom_tool_call", "call_id": "patch-a", "name": "apply_patch", "input": patch}})
	var doc struct{ Input []struct{ Input string } }
	if json.Unmarshal(out, &doc) != nil {
		t.Fatal("decode failed")
	}
	kind := "response.custom_tool_call_input.delta"
	var events []sse.Event
	for _, part := range doc.Input[0].Input {
		events = append(events, ev(kind, string(semanticJSON(t, map[string]any{"type": kind, "item_id": "patch-a", "delta": string(part)}))))
	}
	restored := deltas(t, feed(t, New().NewResponseRewriter(e.r), events), kind)
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.txt")
	if os.WriteFile(path, []byte("before\n"), 0600) != nil {
		t.Fatal("initial file failed")
	}
	command := exec.Command("git", "apply", "--whitespace=error", "-")
	command.Dir = dir
	command.Stdin = strings.NewReader(restored)
	if command.Run() != nil {
		t.Fatal("restored patch could not be applied")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != desired {
		t.Fatal("patch changed wrong content")
	}
}
