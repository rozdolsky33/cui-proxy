package transform

import "strings"

// ctxLen is how many already-emitted bytes the boundary rule looks back at.
const ctxLen = 2

// Stream restores a response that arrives in chunks. For any chunking of a
// text, the concatenation of every Write result and Flush equals
// Restorer.Text of the whole text (given the vault does not change meanwhile).
// It holds back only the shortest tail whose restoration could still change:
// a tail that is a prefix of some synthetic (vault.HasSyntheticWithPrefix
// semantics) or a complete synthetic whose right boundary (see Restorer) is
// not yet known. A Stream is not safe for concurrent use.
type Stream struct {
	r   *Restorer
	ctx string // last ctxLen bytes already emitted (original, unrestored text)
	buf string // held-back text not yet emitted
}

// NewStream returns a streaming restorer.
func (r *Restorer) NewStream() *Stream { return &Stream{r: r} }

// Write adds chunk and returns the restored text that is safe to emit now.
func (st *Stream) Write(chunk string) string {
	st.buf += chunk
	return st.emit(false)
}

// Flush returns the remaining held-back text, restored, and resets the stream.
func (st *Stream) Flush() string {
	out := st.emit(true)
	st.ctx = ""
	return out
}

func (st *Stream) emit(final bool) string {
	if st.buf == "" {
		return ""
	}
	t := st.ctx + st.buf
	from := len(st.ctx)
	ms, stop := st.r.snapshot().scan(t, from, final)
	out := st.r.apply(t, from, stop, ms)
	c := t[:stop]
	if len(c) > ctxLen {
		c = c[len(c)-ctxLen:]
	}
	// Clone so the small retained pieces do not pin the whole chunk in memory.
	st.ctx, st.buf = strings.Clone(c), strings.Clone(t[stop:])
	return out
}
