package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEscapedToolOutputHTTPRoundTrip(t *testing.T) {
	real := "\ngit@github.com:example/demo.git\n"
	inner, _ := json.Marshal(map[string]string{"output": real})
	body, _ := json.Marshal(map[string]any{"input": []any{map[string]string{"type": "custom_tool_call_output", "call_id": "c1", "output": string(inner)}}})
	var h *harness
	h = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		raw := h.upstreamGot().Body
		var req struct{ Input []struct{ Output string } }
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(string(raw), "git@github.com") {
			t.Error("real email reached upstream")
		}
		args, _ := json.Marshal(map[string]string{"payload": req.Input[0].Output})
		sseHeaders(w)
		for _, part := range []string{string(args[:len(args)/2]), string(args[len(args)/2:])} {
			data, _ := json.Marshal(map[string]string{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": part})
			writeEvent(w, "response.function_call_arguments.delta", string(data))
		}
		data, _ := json.Marshal(map[string]string{"type": "response.function_call_arguments.done", "item_id": "fc_1", "arguments": string(args)})
		writeEvent(w, "response.function_call_arguments.done", string(data))
	})
	res, out := h.post(string(body))
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var deltas strings.Builder
	for _, event := range readEvents(t, out) {
		var data map[string]string
		if err := json.Unmarshal(event.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data["type"] == "response.function_call_arguments.delta" {
			deltas.WriteString(data["delta"])
		}
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(deltas.String()), &args); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(args["payload"]), &result); err != nil {
		t.Fatal(err)
	}
	if result["output"] != real {
		t.Fatal("round trip changed tool output")
	}
	a := h.nextAudit()
	if a.Blocked != "" || a.Rules["builtin.email"] != 1 {
		t.Fatalf("unexpected audit: %+v", a)
	}
	if strings.Contains(h.logs.String(), "git@github.com") {
		t.Fatal("value leaked to log")
	}
}

func TestIncompleteArgumentEOFAbortsClient(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		writeEvent(w, "response.created", evCreated)
		writeEvent(w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"f1","delta":"{\"cmd\":\"unfinished"}`)
	})
	res, err := h.client.Do(h.req("POST", "/v1/responses", userBody("hi")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil {
		t.Fatal("incomplete arguments ended cleanly")
	}
	if a := h.nextAudit(); a.Blocked != "stream_rewrite" {
		t.Fatalf("audit = %+v", a)
	}
	if strings.Contains(h.logs.String(), "unfinished") {
		t.Fatal("argument leaked into log")
	}
}
