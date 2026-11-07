// Package sse implements minimal Server-Sent Events framing: a Reader that
// parses events from a stream and a Write function that serializes them.
package sse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxEventSize bounds the bytes buffered for a single event (all of its
// lines, including field names and comments). Next returns ErrEventTooLarge
// when an event exceeds it. It is 64 MiB by default; it is a variable only so
// tests can lower it.
var MaxEventSize = 64 << 20

// ErrEventTooLarge is returned by Reader.Next when one event exceeds MaxEventSize.
var ErrEventTooLarge = errors.New("sse: event exceeds maximum size")

// Event is one SSE event. Data is the "data:" lines joined with "\n".
type Event struct {
	Name string
	Data []byte
}

// Reader parses SSE events. The id: and retry: fields and comment lines are
// accepted and ignored.
type Reader struct {
	br     *bufio.Reader
	skipLF bool // previous line ended in \r; drop a leading \n
	n      int  // bytes consumed, saturating at 4 (for BOM detection)
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// NewReader returns a Reader reading from r.
func NewReader(r io.Reader) *Reader { return &Reader{br: bufio.NewReader(r)} }

// readLine returns the next line without its terminator (\n, \r\n or \r).
// A non-empty partial line at EOF is returned with a nil error and the next
// call reports io.EOF. budget is decremented by the bytes consumed.
func (r *Reader) readLine(budget *int) (line []byte, err error) {
	var buf []byte
	for {
		c, e := r.br.ReadByte()
		if e != nil {
			if e == io.EOF && len(buf) > 0 {
				return buf, nil
			}
			return nil, e
		}
		if r.skipLF {
			r.skipLF = false
			if c == '\n' {
				continue
			}
		}
		if r.n < 4 {
			r.n++
		}
		*budget--
		if *budget < 0 {
			return nil, ErrEventTooLarge
		}
		switch c {
		case '\n':
			return buf, nil
		case '\r':
			r.skipLF = true
			return buf, nil
		}
		buf = append(buf, c)
		if r.n == 3 && bytes.Equal(buf, bom) {
			buf = buf[:0] // strip a leading UTF-8 BOM, once
		}
	}
}

// Next returns the next event, or io.EOF when the stream has ended. A final
// event not followed by a blank line is still returned.
func (r *Reader) Next() (Event, error) {
	var (
		name    string
		data    []byte
		hasData bool
		started bool
	)
	budget := MaxEventSize
	for {
		line, err := r.readLine(&budget)
		if err != nil {
			if err == io.EOF && started {
				return Event{Name: name, Data: data}, nil
			}
			return Event{}, err
		}
		if len(line) == 0 {
			if started {
				return Event{Name: name, Data: data}, nil
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte{':'})
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		switch string(field) {
		case "event":
			name = string(value)
			started = true
		case "data":
			if hasData {
				data = append(data, '\n')
			}
			data = append(data, value...)
			hasData = true
			started = true
		}
	}
}

// Write serializes e as "event: name\ndata: ...\n\n". Multi-line Data is
// written as multiple data: lines. The whole event is issued in a single
// Write call. Line terminators in Data (\n, \r\n, \r) all become line breaks;
// a line terminator in Name is an error.
func Write(w io.Writer, e Event) error {
	if strings.ContainsAny(e.Name, "\r\n") {
		return fmt.Errorf("sse: event name contains a line break")
	}
	var b bytes.Buffer
	if e.Name != "" {
		b.WriteString("event: ")
		b.WriteString(e.Name)
		b.WriteByte('\n')
	}
	rest := e.Data
	for {
		i := bytes.IndexAny(rest, "\r\n")
		b.WriteString("data: ")
		if i < 0 {
			b.Write(rest)
			b.WriteByte('\n')
			break
		}
		b.Write(rest[:i])
		b.WriteByte('\n')
		n := i + 1
		if rest[i] == '\r' && n < len(rest) && rest[n] == '\n' {
			n++
		}
		rest = rest[n:]
	}
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}
