package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOutputPlanMatchesDirectAndOwnsBytes(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		raw := []byte(`{"same":1e+02,"same":true,"nested":[null,"raw"],"padding":"` + strings.Repeat("a", 4096) + `","a":"a","b":"b","c":"c","d":"d"}`)
		n, err := parse(raw, 0)
		if err != nil {
			t.Fatal("parse")
		}
		n.dirty = true
		n.kids[3].setString(strings.Repeat("\x00\xff<>&\u2028\n", 1000))
		var direct bytes.Buffer
		n.write(&direct, canonical)
		count := n.outputPartCount(canonical, maxOutputParts)
		plan := &jsonOutputPlan{parts: make([]jsonOutputPart, 0, count)}
		n.emit(plan, canonical)
		if len(plan.parts) != count {
			t.Fatal("part count")
		}
		got := n.encode(canonical)
		if !bytes.Equal(got, direct.Bytes()) || !bytes.Equal(got, plan.bytes()) || !json.Valid(got) {
			t.Fatal("output mismatch")
		}
		if cap(got) != len(got) {
			t.Fatal("planned output not exact capacity")
		}
		for i := range raw {
			raw[i] = ' '
		}
		for i := 0; i < 20; i++ {
			_ = quote(strings.Repeat("different", 2000))
		}
		if !bytes.Equal(got, direct.Bytes()) {
			t.Fatal("output aliases source or encoder storage")
		}
	}
}

func TestOutputPlanBoundsAndShrink(t *testing.T) {
	n := &node{kind: '[', dirty: true, raw: make([]byte, 512<<10)}
	for i := 0; i < 20; i++ {
		n.kids = append(n.kids, &node{kind: '"', str: "x", dirty: true})
	}
	for _, limit := range []int{0, 1, 2, 15, 41, 42, maxOutputParts} {
		if got := n.outputPartCount(false, limit); got != min(41, limit) {
			t.Fatal("count saturation")
		}
	}
	got := n.encode(false)
	if len(got) != 81 || cap(got) != 81 {
		t.Fatal("discarded input influenced output capacity")
	}
	n.dirty = false
	if n.outputPartCount(false, maxOutputParts) != 1 {
		t.Fatal("raw node count")
	}
	n.dirty = true
	for len(n.kids) < maxOutputParts {
		n.kids = append(n.kids, n.kids[0])
	}
	if n.outputPartCount(false, maxOutputParts) != maxOutputParts {
		t.Fatal("fragment bound")
	}
	var direct bytes.Buffer
	n.write(&direct, false)
	if !bytes.Equal(n.encode(false), direct.Bytes()) {
		t.Fatal("fragment fallback")
	}
}

func TestOutputPlanOverflow(t *testing.T) {
	p := &jsonOutputPlan{size: int(^uint(0) >> 1)}
	defer func() {
		if recover() == nil {
			t.Fatal("expected overflow rejection")
		}
	}()
	p.token(',')
}

func FuzzOutputPlanStandardEncoder(f *testing.F) {
	for _, s := range []string{"", "plain", "\x00\xff\n<>&\u2028\u2029", strings.Repeat("long", 2000)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 1<<16 {
			t.Skip()
		}
		values := make([]string, 8)
		n := &node{kind: '[', dirty: true, raw: make([]byte, 4096)}
		for i := range values {
			values[i] = s
			n.kids = append(n.kids, &node{kind: '"', str: s, dirty: true})
		}
		var want bytes.Buffer
		e := json.NewEncoder(&want)
		e.SetEscapeHTML(false)
		if e.Encode(values) != nil {
			t.Fatal("oracle encode")
		}
		for _, canonical := range []bool{false, true} {
			if !bytes.Equal(n.encode(canonical), bytes.TrimSuffix(want.Bytes(), []byte{'\n'})) {
				t.Fatal("differential mismatch")
			}
		}
	})
}

func TestUnchangedOutputSingleAllocation(t *testing.T) {
	raw := []byte(`{"padding":"` + strings.Repeat("x", 4096) + `"}`)
	n, err := parse(raw, 0)
	if err != nil {
		t.Fatal("parse")
	}
	if allocations := testing.AllocsPerRun(50, func() {
		got := n.encode(false)
		if !bytes.Equal(got, raw) {
			panic("output mismatch")
		}
	}); allocations != 1 {
		t.Fatal("unchanged output must retain one owned-copy allocation")
	}
	got := n.encode(false)
	raw[0] = ' '
	if got[0] != '{' {
		t.Fatal("raw output aliases input")
	}
}
