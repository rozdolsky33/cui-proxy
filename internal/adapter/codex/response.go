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
	name     string // SSE event name of the last delta
	last     *node  // the last delta event, reused for trailing deltas
	deltaAt  int    // index of "delta" in last
	item     string // raw item_id
	outIndex string // raw output_index
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
//
// An event nesting deeper than maxDepth returns ErrDepth (cut the stream).
func (rw *rewriter) Event(e sse.Event) ([]sse.Event, error) {
	pass := []sse.Event{e}
	if first(e.Data) != '{' || !json.Valid(e.Data) {
		return pass, nil
	}
	root, err := parse(e.Data, 0)
	if err != nil {
		return nil, err
	}
	typ, _ := root.getString("type")
	if _, ok := deltaKinds[typ]; ok {
		return rw.delta(e, root, typ)
	}
	if strings.HasSuffix(typ, ".delta") || strings.Contains(typ, "reasoning") && !strings.Contains(typ, "reasoning_summary") {
		return pass, nil
	}
	out := rw.release(typ, root)
	if rw.restore(root, ""); root.dirty {
		e.Data = root.encode(false)
	}
	return append(out, e), nil
}

// Flush releases the held-back text of every open stream.
func (rw *rewriter) Flush() []sse.Event {
	return rw.flushWhere(func(string, *openStream) bool { return true })
}

func rawOf(n *node) string {
	if n == nil {
		return ""
	}
	return string(n.raw)
}

func streamKey(kind string, n *node) string {
	var b strings.Builder
	b.WriteString(kind)
	for _, k := range []string{"item_id", "output_index", "content_index", "summary_index"} {
		b.WriteByte(0)
		b.WriteString(rawOf(n.get(k)))
	}
	return b.String()
}

func (rw *rewriter) delta(e sse.Event, root *node, typ string) ([]sse.Event, error) {
	at := -1
	for i, k := range root.keys {
		if k == "delta" {
			at = i
		}
	}
	if at < 0 || root.kids[at].kind != '"' {
		return nil, fmt.Errorf("codex: %s event without a string delta", typ)
	}
	d := root.kids[at].str
	key := streamKey(typ, root)
	os := rw.streams[key]
	if os == nil {
		r := rw.plain
		if deltaKinds[typ] {
			r = rw.esc
		}
		os = &openStream{st: r.NewStream(), item: rawOf(root.get("item_id")), outIndex: rawOf(root.get("output_index"))}
		rw.streams[key] = os
		rw.order = append(rw.order, key)
	}
	os.name, os.last, os.deltaAt = e.Name, root, at
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
	ev := *os.last
	ev.kids = append([]*node(nil), os.last.kids...)
	ev.kids[os.deltaAt] = &node{kind: '"', str: d, dirty: true}
	ev.dirty = true
	return sse.Event{Name: os.name, Data: ev.encode(false)}
}

// release flushes the streams that the event typ closes.
func (rw *rewriter) release(typ string, root *node) []sse.Event {
	switch {
	case terminal[typ]:
		return rw.Flush()
	case typ == "response.output_item.done":
		var id string
		if item := root.get("item"); item != nil && item.kind == '{' {
			id = rawOf(item.get("id"))
		}
		idx := rawOf(root.get("output_index"))
		return rw.flushWhere(func(_ string, os *openStream) bool {
			return id != "" && os.item == id || idx != "" && os.outIndex == idx
		})
	case partDone[typ] != nil:
		keys := map[string]bool{}
		for _, k := range partDone[typ] {
			keys[streamKey(k, root)] = true
		}
		return rw.flushWhere(func(k string, _ *openStream) bool { return keys[k] })
	case strings.HasSuffix(typ, ".done"):
		k := streamKey(strings.TrimSuffix(typ, ".done")+".delta", root)
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

// restore restores the string leaves of n in place. key is the member name
// n is the value of ("" for array elements and the event itself).
func (rw *rewriter) restore(n *node, key string) {
	switch n.kind {
	case '{':
		t, _ := n.getString("type")
		reasoning := t == "reasoning"
		for i, k := range n.keys {
			if opaque[k] || reasoning && k != "summary" {
				continue
			}
			rw.restore(n.kids[i], k)
			n.dirty = n.dirty || n.kids[i].dirty
		}
	case '[':
		for _, kid := range n.kids {
			rw.restore(kid, "")
			n.dirty = n.dirty || kid.dirty
		}
	case '"':
		r := rw.plain
		if key == "arguments" {
			r = rw.esc
		}
		if out := r.Text(n.str); out != n.str {
			n.setString(out)
		}
	}
}
