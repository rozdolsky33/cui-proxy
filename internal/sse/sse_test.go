package sse

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func readAll(t *testing.T, in string) []Event {
	t.Helper()
	r := NewReader(strings.NewReader(in))
	var out []Event
	for {
		e, err := r.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, e)
	}
}

func TestReadSingleAndMultilineData(t *testing.T) {
	evs := readAll(t, "event: a\ndata: {\"x\":1}\n\nevent: b\ndata: l1\ndata: l2\ndata:\ndata: l4\n\n")
	if len(evs) != 2 {
		t.Fatalf("got %d events", len(evs))
	}
	if evs[0].Name != "a" || string(evs[0].Data) != `{"x":1}` {
		t.Fatalf("ev0 %+v", evs[0])
	}
	if evs[1].Name != "b" || string(evs[1].Data) != "l1\nl2\n\nl4" {
		t.Fatalf("ev1 %q", evs[1].Data)
	}
}

func TestReadCRLF(t *testing.T) {
	for _, nl := range []string{"\r\n", "\r"} {
		evs := readAll(t, "event: a"+nl+"data: x"+nl+"data: y"+nl+nl+"data: z"+nl+nl)
		if len(evs) != 2 || evs[0].Name != "a" || string(evs[0].Data) != "x\ny" || string(evs[1].Data) != "z" {
			t.Fatalf("nl %q: %+v", nl, evs)
		}
	}
}

func TestReadIgnoresComments(t *testing.T) {
	evs := readAll(t, ": hi\nid: 7\nretry: 100\nevent: a\n: mid\ndata: x\nbogus\n\n")
	if len(evs) != 1 || evs[0].Name != "a" || string(evs[0].Data) != "x" {
		t.Fatalf("%+v", evs)
	}
}

func TestReadSpaceStripping(t *testing.T) {
	evs := readAll(t, "data:  two\n\ndata:none\n\n")
	if string(evs[0].Data) != " two" || string(evs[1].Data) != "none" {
		t.Fatalf("%q %q", evs[0].Data, evs[1].Data)
	}
}

func TestReadEOFWithoutBlankLine(t *testing.T) {
	evs := readAll(t, "event: a\ndata: x")
	if len(evs) != 1 || string(evs[0].Data) != "x" {
		t.Fatalf("%+v", evs)
	}
}

func TestWriteRoundTrip(t *testing.T) {
	in := []Event{
		{Name: "response.output_text.delta", Data: []byte(`{"d":"hi"}`)},
		{Name: "m", Data: []byte("a\nb\n\nc")},
		{Data: []byte("noname")},
		{Name: "empty", Data: nil},
		{Name: "trail", Data: []byte("x\n")},
	}
	var buf bytes.Buffer
	for _, e := range in {
		if err := Write(&buf, e); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.HasPrefix(buf.String(), "event: response.output_text.delta\ndata: {\"d\":\"hi\"}\n\n") {
		t.Fatalf("format: %q", buf.String())
	}
	got := readAll(t, buf.String())
	if len(got) != len(in) {
		t.Fatalf("got %d", len(got))
	}
	for i := range in {
		if got[i].Name != in[i].Name || !bytes.Equal(got[i].Data, in[i].Data) {
			t.Fatalf("%d: %+v vs %+v", i, got[i], in[i])
		}
	}
}

type shortWriter struct{}

var errShort = errors.New("short")

func (s *shortWriter) Write(p []byte) (int, error) { return 0, errShort }

func TestWriteError(t *testing.T) {
	if err := Write(&shortWriter{}, Event{Name: "a", Data: []byte("x")}); !errors.Is(err, errShort) {
		t.Fatalf("err %v", err)
	}
	if err := Write(io.Discard, Event{Name: "a\nb"}); err == nil {
		t.Fatal("expected error for newline in name")
	}
}

func TestReadLargeEvent(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	evs := readAll(t, "event: big\ndata: "+big+"\n\n")
	if len(evs) != 1 || len(evs[0].Data) != 1<<20 {
		t.Fatalf("bad large read")
	}
}

func TestReadOverCap(t *testing.T) {
	old := MaxEventSize
	MaxEventSize = 1024
	defer func() { MaxEventSize = old }()
	r := NewReader(strings.NewReader("data: " + strings.Repeat("x", 4096) + "\n\n"))
	if _, err := r.Next(); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("err %v", err)
	}
	// many small lines also count toward the cap
	r = NewReader(strings.NewReader(strings.Repeat("data: xxxxxxxx\n", 200) + "\n"))
	if _, err := r.Next(); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("err %v", err)
	}
}

type oneByteReader struct{ s string }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(o.s) == 0 {
		return 0, io.EOF
	}
	p[0] = o.s[0]
	o.s = o.s[1:]
	return 1, nil
}

func TestReadSplitAcrossReads(t *testing.T) {
	r := NewReader(&oneByteReader{"event: a\r\ndata: x\r\n\r\ndata: y\r\rdata: z\n\n"})
	var got []string
	for {
		e, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, e.Name+"|"+string(e.Data))
	}
	if strings.Join(got, ",") != "a|x,|y,|z" {
		t.Fatalf("%q", got)
	}
}

func TestReadDoesNotBlockAfterCR(t *testing.T) {
	for _, in := range []string{"data: x\r\r", "data: x\r\n\r", "data: x\n\n", "data: x\r\n\r\n"} {
		pr, pw := io.Pipe()
		go func() { _, _ = pw.Write([]byte(in)) }() // then stays open
		type res struct {
			e   Event
			err error
		}
		ch := make(chan res, 1)
		go func() {
			e, err := NewReader(pr).Next()
			ch <- res{e, err}
		}()
		select {
		case r := <-ch:
			if r.err != nil || string(r.e.Data) != "x" {
				t.Fatalf("%q: %+v", in, r)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%q: Next blocked on a complete event", in)
		}
		pw.Close()
	}
}

// errAfter returns its chunks, then err once, then io.EOF.
type errAfter struct {
	chunks []string
	err    error
	done   bool
}

func (e *errAfter) Read(p []byte) (int, error) {
	if len(e.chunks) == 0 {
		if e.done {
			return 0, io.EOF
		}
		e.done = true
		return 0, e.err
	}
	n := copy(p, e.chunks[0])
	e.chunks = e.chunks[1:]
	return n, nil
}

func TestReadPropagatesReadErrorAfterCR(t *testing.T) {
	boom := errors.New("boom")
	r := NewReader(&errAfter{chunks: []string{"data: x\r"}, err: boom})
	if _, err := r.Next(); !errors.Is(err, boom) && err != nil {
		t.Fatalf("err %v", err)
	}
	// event was complete only if terminated by blank line; here it is not,
	// so the read error must surface and no event is returned.
	r = NewReader(&errAfter{chunks: []string{"data: x\r"}, err: boom})
	e, err := r.Next()
	if !errors.Is(err, boom) || e.Data != nil || e.Name != "" {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func TestReadErrorMidEvent(t *testing.T) {
	boom := errors.New("boom")
	r := NewReader(&errAfter{chunks: []string{"event: a\ndata: par"}, err: boom})
	e, err := r.Next()
	if !errors.Is(err, boom) || e.Name != "" || e.Data != nil {
		t.Fatalf("got %+v, %v", e, err)
	}
	r = NewReader(&errAfter{chunks: []string{"event: a\ndata: x\n"}, err: boom})
	if e, err = r.Next(); !errors.Is(err, boom) || e.Data != nil {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func TestReadNameOnlyEvent(t *testing.T) {
	evs := readAll(t, "event: ping\n\n")
	if len(evs) != 1 || evs[0].Name != "ping" || len(evs[0].Data) != 0 {
		t.Fatalf("%+v", evs)
	}
}

func TestReadLeadingBOM(t *testing.T) {
	evs := readAll(t, "\xEF\xBB\xBFevent: a\ndata: x\n\n\xEF\xBB\xBFdata: y\n\n")
	if len(evs) < 1 || evs[0].Name != "a" || string(evs[0].Data) != "x" {
		t.Fatalf("%+v", evs)
	}
	if len(evs) != 1 {
		t.Fatalf("second BOM must not be stripped (unknown field, no event): %+v", evs)
	}
	r := NewReader(&oneByteReader{"\xEF\xBB\xBFdata: z\n\n"})
	if e, err := r.Next(); err != nil || string(e.Data) != "z" {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestReadBOMOnlyAtStreamStart(t *testing.T) {
	evs := readAll(t, "\n\xEF\xBB\xBFdata: y\n\ndata: z\n\n")
	if len(evs) != 1 || string(evs[0].Data) != "z" {
		t.Fatalf("%+v", evs)
	}
}
