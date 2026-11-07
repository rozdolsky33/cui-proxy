package sse

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
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
