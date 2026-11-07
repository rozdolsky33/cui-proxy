package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"cui-proxy/internal/sse"
	"cui-proxy/internal/transform"
)

// deltaKinds are the streamed text events that are restored. The value
// reports whether the text is JSON (function-call arguments), so restored
// real values are JSON-escaped. custom_tool_call_input carries free-form
// tool input (Codex's apply_patch) and is restored as plain text.
var deltaKinds = map[string]bool{
	"response.output_text.delta":             false,
	"response.refusal.delta":                 false,
	"response.reasoning_summary_text.delta":  false,
	"response.function_call_arguments.delta": true,
	"response.custom_tool_call_input.delta":  false,
}

// partDone maps a part-level done event to the delta kind it closes.
var partDone = map[string][]string{
	"response.content_part.done":           {"response.output_text.delta", "response.refusal.delta"},
	"response.reasoning_summary_part.done": {"response.reasoning_summary_text.delta"},
}

// terminal events release every open stream first.
var terminal = map[string]bool{
	"response.completed": true, "response.failed": true, "response.incomplete": true, "error": true,
}

// openStream is one in-progress streamed text field.
type openStream struct {
	st       *transform.Stream
	name     string   // SSE event name of the last delta
	last     []member // members of the last delta event, reused for trailing deltas
	deltaAt  int      // index of "delta" in last
	item     string   // raw item_id
	outIndex string   // raw output_index
}

type rewriter struct {
	plain, esc *transform.Restorer
	streams    map[string]*openStream
	order      []string // keys in creation order, for deterministic flushing
}

func newRewriter(r *transform.Restorer) *rewriter {
	return &rewriter{plain: r, esc: r.JSONEscaped(), streams: map[string]*openStream{}}
}

// Event restores one SSE event.
//
//   - Known delta events stream through a per-field Stream (held-back text may
//     yield zero events).
//   - Unknown delta events, reasoning events other than summaries, and data
//     that is not a JSON object pass through as the original bytes.
//   - Every other event has its string leaves restored, except opaque keys and
//     reasoning items' fields other than summary; it is re-encoded only when
//     something changed. Done events first release the held-back text of the
//     streams they close as a trailing delta event.
func (rw *rewriter) Event(e sse.Event) ([]sse.Event, error) {
	pass := []sse.Event{e}
	if first(e.Data) != '{' || !json.Valid(e.Data) {
		return pass, nil
	}
	ms, err := members(e.Data)
	if err != nil {
		return pass, nil
	}
	typ, _ := getString(ms, "type")
	if _, ok := deltaKinds[typ]; ok {
		return rw.delta(e, ms, typ)
	}
	if strings.HasSuffix(typ, ".delta") || strings.Contains(typ, "reasoning") && !strings.Contains(typ, "reasoning_summary") {
		return pass, nil
	}
	out := rw.release(typ, ms)
	if raw, changed := rw.restore(e.Data, ""); changed {
		e.Data = raw
	}
	return append(out, e), nil
}

// Flush releases the held-back text of every open stream.
func (rw *rewriter) Flush() []sse.Event {
	return rw.flushWhere(func(string, *openStream) bool { return true })
}

func streamKey(kind string, ms []member) string {
	var b strings.Builder
	b.WriteString(kind)
	for _, k := range []string{"item_id", "output_index", "content_index", "summary_index"} {
		raw, _ := get(ms, k)
		b.WriteByte(0)
		b.Write(raw)
	}
	return b.String()
}

func (rw *rewriter) delta(e sse.Event, ms []member, typ string) ([]sse.Event, error) {
	at := -1
	for i, m := range ms {
		if m.key == "delta" {
			at = i
		}
	}
	var d string
	if at < 0 || first(ms[at].val) != '"' || json.Unmarshal(ms[at].val, &d) != nil {
		return nil, fmt.Errorf("codex: %s event without a string delta", typ)
	}
	key := streamKey(typ, ms)
	os := rw.streams[key]
	if os == nil {
		r := rw.plain
		if deltaKinds[typ] {
			r = rw.esc
		}
		item, _ := get(ms, "item_id")
		out, _ := get(ms, "output_index")
		os = &openStream{st: r.NewStream(), item: string(item), outIndex: string(out)}
		rw.streams[key] = os
		rw.order = append(rw.order, key)
	}
	os.name, os.last, os.deltaAt = e.Name, ms, at
	out := os.st.Write(d)
	switch out {
	case d:
		return []sse.Event{e}, nil
	case "":
		return nil, nil
	}
	return []sse.Event{os.withDelta(out)}, nil
}

// withDelta returns the last delta event with its delta replaced.
func (os *openStream) withDelta(d string) sse.Event {
	ms := append([]member(nil), os.last...)
	ms[os.deltaAt].val = quote(d)
	return sse.Event{Name: os.name, Data: encodeObject(ms)}
}

// release flushes the streams that the event typ (with members ms) closes.
func (rw *rewriter) release(typ string, ms []member) []sse.Event {
	switch {
	case terminal[typ]:
		return rw.Flush()
	case typ == "response.output_item.done":
		var id, idx []byte
		if item, ok := get(ms, "item"); ok && first(item) == '{' {
			if ims, err := members(item); err == nil {
				id, _ = get(ims, "id")
			}
		}
		idx, _ = get(ms, "output_index")
		return rw.flushWhere(func(_ string, os *openStream) bool {
			return len(id) > 0 && os.item == string(id) || len(idx) > 0 && os.outIndex == string(idx)
		})
	case partDone[typ] != nil:
		keys := map[string]bool{}
		for _, k := range partDone[typ] {
			keys[streamKey(k, ms)] = true
		}
		return rw.flushWhere(func(k string, _ *openStream) bool { return keys[k] })
	case strings.HasSuffix(typ, ".done"):
		k := streamKey(strings.TrimSuffix(typ, ".done")+".delta", ms)
		return rw.flushWhere(func(key string, _ *openStream) bool { return key == k })
	}
	return nil
}

// flushWhere flushes and closes the streams selected by sel, in creation order.
func (rw *rewriter) flushWhere(sel func(string, *openStream) bool) []sse.Event {
	var out []sse.Event
	order := rw.order[:0]
	for _, k := range rw.order {
		os := rw.streams[k]
		if !sel(k, os) {
			order = append(order, k)
			continue
		}
		if rest := os.st.Flush(); rest != "" {
			out = append(out, os.withDelta(rest))
		}
		delete(rw.streams, k)
	}
	rw.order = order
	return out
}

// restore restores the string leaves of raw. key is the member name raw is
// the value of ("" for array elements and the event itself).
func (rw *rewriter) restore(raw json.RawMessage, key string) (json.RawMessage, bool) {
	switch first(raw) {
	case '{':
		ms, err := members(raw)
		if err != nil {
			return raw, false
		}
		t, _ := getString(ms, "type")
		reasoning := t == "reasoning"
		changed := false
		for i, m := range ms {
			if opaque[m.key] || reasoning && m.key != "summary" {
				continue
			}
			if nv, ch := rw.restore(m.val, m.key); ch {
				ms[i].val, changed = nv, true
			}
		}
		if !changed {
			return raw, false
		}
		return encodeObject(ms), true
	case '[':
		es, err := elements(raw)
		if err != nil {
			return raw, false
		}
		changed := false
		for i, e := range es {
			if ne, ch := rw.restore(e, ""); ch {
				es[i], changed = ne, true
			}
		}
		if !changed {
			return raw, false
		}
		return encodeArray(es), true
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return raw, false
		}
		r := rw.plain
		if key == "arguments" {
			r = rw.esc
		}
		if out := r.Text(s); out != s {
			return quote(out), true
		}
	}
	return raw, false
}
