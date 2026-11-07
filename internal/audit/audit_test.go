package audit

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRingBufferEvictsOldest(t *testing.T) {
	l := New(3)
	for i := 1; i <= 5; i++ {
		l.Add(Entry{Status: i})
	}
	got := l.Recent()
	if len(got) != 3 || got[0].Status != 3 || got[2].Status != 5 {
		t.Fatalf("got %+v", got)
	}
	if len(New(0).Recent()) != 0 {
		t.Fatal("non-empty")
	}
	New(0).Add(Entry{}) // must not panic
}

func TestRecentIsCopy(t *testing.T) {
	l := New(2)
	l.Add(Entry{Status: 1, Rules: map[string]int{"a": 1}})
	r := l.Recent()
	r[0].Status = 9
	r[0].Rules["a"] = 9
	if g := l.Recent()[0]; g.Status != 1 || g.Rules["a"] != 1 {
		t.Fatalf("aliased: %+v", g)
	}
}

func TestSubscribeReceivesAndUnsubscribes(t *testing.T) {
	l := New(10)
	ch, cancel := l.Subscribe()
	l.Add(Entry{Status: 200})
	select {
	case e := <-ch:
		if e.Status != 200 {
			t.Fatalf("%+v", e)
		}
	default:
		t.Fatal("no entry delivered")
	}
	cancel()
	cancel() // idempotent
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed")
	}
	l.Add(Entry{Status: 201}) // must not panic after cancel

	// Slow subscriber never blocks Add.
	slow, cancel2 := l.Subscribe()
	defer cancel2()
	done := make(chan struct{})
	go func() {
		for i := 0; i < subBuffer*3; i++ {
			l.Add(Entry{Status: i})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Add blocked on slow subscriber")
	}
	if len(slow) != subBuffer {
		t.Fatalf("buffered=%d", len(slow))
	}
	if l.Dropped() != uint64(subBuffer*2) {
		t.Fatalf("dropped=%d", l.Dropped())
	}
}

func TestEntryHasNoValueFields(t *testing.T) {
	typ := reflect.TypeOf(Entry{})
	for i := 0; i < typ.NumField(); i++ {
		n := strings.ToLower(typ.Field(i).Name)
		for _, bad := range []string{"value", "real", "body", "text"} {
			if strings.Contains(n, bad) {
				t.Fatalf("forbidden field %s", typ.Field(i).Name)
			}
		}
	}
	want := []string{"Time", "RequestID", "Adapter", "Method", "Path", "Status", "BytesIn", "BytesOut", "Rules", "Blocked", "LatencyMS"}
	if typ.NumField() != len(want) {
		t.Fatalf("fields changed: %d", typ.NumField())
	}
	for i, w := range want {
		if typ.Field(i).Name != w {
			t.Fatalf("field %d = %s want %s", i, typ.Field(i).Name, w)
		}
	}
}
