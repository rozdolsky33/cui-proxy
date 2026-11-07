package decisionlog

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)} }

type logLines struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logLines) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format+"\n", args...)
}

func (l *logLines) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func open(t *testing.T, o Options) *Log {
	t.Helper()
	l, err := Open(o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func entry(i int) Entry {
	e := Entry{RequestID: NewID(), Adapter: "codex", Profile: "sanitize", Decision: Forwarded}
	switch i % 3 {
	case 1:
		e.Decision, e.Counts = Sanitized, map[string]int{"builtin.ipv4": i, "hosts-a": 1}
	case 2:
		e.Decision, e.Reason, e.Counts = Blocked, "policy", map[string]int{"builtin.ssn": 1}
	}
	return e
}

func write(t *testing.T, l *Log, n int) {
	t.Helper()
	for i := range n {
		if err := l.Record(entry(i)); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
}

func verifyOK(t *testing.T, path string, records int) *Report {
	t.Helper()
	rep, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !rep.OK() || rep.Records != records {
		t.Fatalf("Verify: ok=%v records=%d (want %d) breaks=%v gaps=%v unverified=%d", rep.OK(), rep.Records, records, rep.Breaks, rep.Gaps, rep.Unverified)
	}
	return rep
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitAfter(string(b), "\n")
}

func TestRecordsVerifyAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "cui-proxy")
	path := filepath.Join(dir, "decisions.jsonl")
	l := open(t, Options{Path: path})
	write(t, l, 7)
	rep := verifyOK(t, path, 7)
	if rep.Start != Genesis || rep.StartFile != path || rep.HeadLine != 7 {
		t.Errorf("start %s in %s, head line %d", rep.Start, rep.StartFile, rep.HeadLine)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", fi.Mode().Perm())
	}
	// Every line is one canonical record whose prev_hash is the previous hash.
	prev := Genesis
	for i, ln := range lines(t, path)[:7] {
		r, err := parseLine([]byte(strings.TrimSuffix(ln, "\n")))
		if err != nil || r.PrevHash != prev {
			t.Fatalf("line %d: %v prev %s want %s", i+1, err, r.PrevHash, prev)
		}
		prev = r.Hash
	}
	if rep.Head != prev {
		t.Errorf("head %s, want %s", rep.Head, prev)
	}
}

func TestExistingWiderModeIsTightened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	open(t, Options{Path: path})
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestRestartContinuesTheChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	write(t, l, 3)
	l.Close()
	l2 := open(t, Options{Path: path})
	write(t, l2, 4)
	l2.Close()
	rep := verifyOK(t, path, 7)
	if rep.Start != Genesis {
		t.Errorf("a restart started a new chain: start %s", rep.Start)
	}
}

// Tampering is detected and located (file and line).
func TestVerifyDetectsTampering(t *testing.T) {
	for _, c := range []struct {
		name     string
		tamper   func([]string) []string
		line     int
		kind     string
		withGaps bool
	}{
		{"edit a value in a line", func(ls []string) []string {
			ls[2] = strings.Replace(ls[2], `"decision":"blocked","reason":"policy"`, `"decision":"forwarded","reason":""`, 1)
			ls[2] = strings.Replace(ls[2], `"rule_ids":["builtin.ssn"],"counts":{"builtin.ssn":1}`, `"rule_ids":[],"counts":{}`, 1)
			return ls
		}, 3, BreakHash, false},
		{"edit a count", func(ls []string) []string {
			ls[1] = strings.Replace(ls[1], `"hosts-a":1`, `"hosts-a":2`, 1)
			return ls
		}, 2, BreakHash, false},
		{"delete a middle line", func(ls []string) []string { return append(ls[:2:2], ls[3:]...) }, 3, BreakPrev, false},
		{"reorder two lines", func(ls []string) []string { ls[1], ls[2] = ls[2], ls[1]; return ls }, 2, BreakPrev, true},
		{"truncate the tail mid-line", func(ls []string) []string {
			last := len(ls) - 2 // ls ends with "" after the final newline
			ls[last] = ls[last][:len(ls[last])/2]
			return ls[:last+1]
		}, 5, BreakTruncated, false},
		{"append a forged line without recomputing", func(ls []string) []string {
			forged := strings.Replace(ls[4], `"profile":"sanitize"`, `"profile":"strict"`, 1)
			return append(ls[:5], forged)
		}, 6, BreakHash, false},
		{"append a copy of the last line", func(ls []string) []string { return append(ls[:5], ls[4]) }, 6, BreakPrev, false},
		{"insert garbage", func(ls []string) []string {
			return append(ls[:1], append([]string{"{\"ts\":1}\n"}, ls[1:]...)...)
		}, 2, BreakMalformed, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "d.jsonl")
			l := open(t, Options{Path: path})
			write(t, l, 5)
			l.Close()
			if err := os.WriteFile(path, []byte(strings.Join(c.tamper(lines(t, path)), "")), 0o600); err != nil {
				t.Fatal(err)
			}
			rep, err := Verify(path)
			if err != nil {
				t.Fatal(err)
			}
			if rep.OK() || len(rep.Breaks) == 0 {
				t.Fatalf("tampering not detected: %+v", rep)
			}
			if b := rep.Breaks[0]; b.Line != c.line || b.Kind != c.kind || b.File != path {
				t.Errorf("first break %v, want line %d %s", b, c.line, c.kind)
			}
			if c.withGaps != (len(rep.Gaps) > 0) {
				t.Errorf("gaps %v", rep.Gaps)
			}
		})
	}
}

// What the chain cannot show without an external anchor (documented): the
// newest lines removed whole, or every hash recomputed.
func TestVerifyLimitsAreAsDocumented(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	write(t, l, 5)
	l.Close()
	ls := lines(t, path)
	if err := os.WriteFile(path, []byte(strings.Join(ls[:3], "")), 0o600); err != nil {
		t.Fatal(err)
	}
	verifyOK(t, path, 3) // tail deletion at a line boundary is not detectable
}

func TestOpenRefusesADamagedFile(t *testing.T) {
	for name, damage := range map[string]func(string) string{
		"corrupt tail":  func(s string) string { return s[:len(s)-10] },
		"edited middle": func(s string) string { return strings.Replace(s, `"hosts-a":1`, `"hosts-a":9`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "d.jsonl")
			l := open(t, Options{Path: path})
			write(t, l, 4)
			l.Close()
			b, _ := os.ReadFile(path)
			if err := os.WriteFile(path, []byte(damage(string(b))), 0o600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			_, err := Open(Options{Path: path})
			if !errors.Is(err, ErrDamaged) || !strings.Contains(err.Error(), "cui-proxy audit verify") {
				t.Fatalf("Open: %v; want ErrDamaged naming `cui-proxy audit verify`", err)
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Error("a refused Open changed the file")
			}
		})
	}
}

// Under warn the damaged file is moved aside and a new segment continues
// from the last good record; Verify reports the break and the gap, never a
// silent pass.
func TestOpenStartsANewSegmentOnDamage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, now: clk.now})
	write(t, l, 4)
	l.Close()
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, b[:len(b)-10], 0o600); err != nil { // cut the last line
		t.Fatal(err)
	}
	logs := &logLines{}
	clk.add(time.Minute)
	l2 := open(t, Options{Path: path, NewSegmentOnDamage: true, Logf: logs.logf, now: clk.now})
	write(t, l2, 2)
	l2.Close()
	if !strings.Contains(logs.String(), "WARN decision log damaged at line 4 (truncated)") {
		t.Errorf("logs = %q", logs.String())
	}
	files, _ := Files(path)
	if len(files) != 2 || files[1] != path {
		t.Fatalf("files %v", files)
	}
	rep, err := Verify(path)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || len(rep.Breaks) != 1 || len(rep.Gaps) != 1 {
		t.Fatalf("report %+v", rep)
	}
	g := rep.Gaps[0]
	if g.AfterFile != files[0] || g.AfterLine != 3 || g.File != path || g.Line != 1 || rep.Records != 5 {
		t.Errorf("gap %+v records %d", g, rep.Records)
	}
}

func TestRotationKeepsTheChainAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	for i := range 20 {
		clk.add(time.Second)
		if err := l.Record(entry(i)); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	files, err := Files(path)
	if err != nil || len(files) < 4 {
		t.Fatalf("files %v %v", files, err)
	}
	for _, f := range files {
		fi, _ := os.Stat(f)
		if fi.Size() > 1200 {
			t.Errorf("%s is %d bytes, over the cap", f, fi.Size())
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, fi.Mode().Perm())
		}
	}
	rep := verifyOK(t, path, 20)
	if rep.Start != Genesis || rep.HeadFile != path {
		t.Errorf("start %s head file %s", rep.Start, rep.HeadFile)
	}
	// Restart after rotation continues the chain.
	l2 := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	write(t, l2, 3)
	l2.Close()
	verifyOK(t, path, 23)

	// A missing middle file is a break.
	if err := os.Remove(files[1]); err != nil {
		t.Fatal(err)
	}
	if rep, _ := Verify(path); rep.OK() || rep.Breaks[0].File != files[2] || rep.Breaks[0].Kind != BreakPrev {
		t.Errorf("a missing middle file was not reported: %+v", rep)
	}
}

func TestRetentionDeletesOldRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.jsonl")
	other := filepath.Join(dir, "d.jsonl.keep-me")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 90, now: clk.now})
	write(t, l, 8) // rotations now
	clk.add(60 * 24 * time.Hour)
	write(t, l, 8) // rotations 60 days later
	l.Close()
	before, _ := Files(path)

	clk.add(40 * 24 * time.Hour) // the first batch is now 100 days old, the second 40
	l2 := open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 90, now: clk.now})
	l2.Close()
	after, _ := Files(path)
	if len(after) >= len(before) || after[len(after)-1] != path {
		t.Fatalf("before %d files, after %d: %v", len(before), len(after), after)
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("retention deleted a file that is not a rotated log file")
	}
	// What survives verifies from a trusted start that is not genesis.
	rep, err := Verify(path)
	if err != nil || !rep.OK() || rep.Start == Genesis || rep.StartFile != after[0] {
		t.Fatalf("after retention: %+v %v", rep, err)
	}

	// retention_days 0 keeps everything.
	clk.add(1000 * 24 * time.Hour)
	l3 := open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 0, now: clk.now})
	l3.Close()
	if kept, _ := Files(path); len(kept) != len(after) {
		t.Errorf("retention 0 deleted files: %d -> %d", len(after), len(kept))
	}
}

func TestConcurrentWritersProduceOneValidChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path, MaxBytes: 16 << 10})
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 25 {
				if err := l.Record(entry(g + i)); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	l.Close()
	verifyOK(t, path, 400)
}

func TestSymlinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, dest := range map[string]string{"to a file": target, "dangling": filepath.Join(dir, "nothing")} {
		path := filepath.Join(t.TempDir(), "d.jsonl")
		if err := os.Symlink(dest, path); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(Options{Path: path}); !errors.Is(err, ErrSymlink) {
			t.Errorf("%s: Open = %v, want ErrSymlink", name, err)
		}
		if _, err := Verify(path); err == nil {
			t.Errorf("%s: Verify followed the symlink", name)
		}
	}
	if b, _ := os.ReadFile(target); string(b) != "keep\n" {
		t.Error("the symlink target was written")
	}
	if _, err := os.Lstat(filepath.Join(dir, "nothing")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a dangling symlink's target was created")
	}
	// The O_NOFOLLOW open itself refuses, even if the Lstat check is raced.
	path := filepath.Join(t.TempDir(), "d.jsonl")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openAppend(path, false); !errors.Is(err, ErrSymlink) {
		t.Errorf("openAppend = %v, want ErrSymlink", err)
	}
}

func TestSecondWriterIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	open(t, Options{Path: path})
	if _, err := Open(Options{Path: path}); !errors.Is(err, ErrInUse) {
		t.Fatalf("second Open = %v, want ErrInUse", err)
	}
}

// failFile is a real file whose next Write or Sync fails (after writing
// half of the line, like a full disk).
type failFile struct {
	*os.File
	failWrite, failSync, failTruncate bool
}

func (f *failFile) Write(b []byte) (int, error) {
	if f.failWrite {
		n, _ := f.File.Write(b[:len(b)/2])
		return n, syscall.ENOSPC
	}
	return f.File.Write(b)
}

func (f *failFile) Sync() error {
	if f.failSync {
		return syscall.EIO
	}
	return f.File.Sync()
}

func (f *failFile) Truncate(n int64) error {
	if f.failTruncate {
		return syscall.EIO
	}
	return f.File.Truncate(n)
}

func openFailing(t *testing.T, path string) (*Log, *failFile) {
	t.Helper()
	var ff *failFile
	l := open(t, Options{Path: path, openFile: func(p string, excl bool) (file, int64, error) {
		f, n, err := openAppend(p, excl)
		if err != nil {
			return nil, 0, err
		}
		ff = &failFile{File: f.(*os.File)}
		return ff, n, nil
	}})
	return l, ff
}

func TestFailedWriteLeavesNoPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l, ff := openFailing(t, path)
	write(t, l, 2)
	for _, c := range []struct {
		name  string
		set   func(bool)
		class string
	}{
		{"write", func(v bool) { ff.failWrite = v }, "no_space"},
		{"sync", func(v bool) { ff.failSync = v }, "io"},
	} {
		c.set(true)
		err := l.Record(entry(0))
		c.set(false)
		if err == nil || ErrClass(err) != c.class {
			t.Fatalf("%s failure: Record = %v (class %s)", c.name, err, ErrClass(err))
		}
		write(t, l, 1) // the log recovers: the failed record left nothing behind
	}
	l.Close()
	verifyOK(t, path, 4)

	// A failure that cannot be undone breaks the log: every later record fails.
	path2 := filepath.Join(t.TempDir(), "d.jsonl")
	l2, ff2 := openFailing(t, path2)
	ff2.failWrite, ff2.failTruncate = true, true
	if err := l2.Record(entry(0)); err == nil {
		t.Fatal("Record succeeded")
	}
	ff2.failWrite, ff2.failTruncate = false, false
	if err := l2.Record(entry(0)); err == nil || !strings.Contains(err.Error(), "restart required") {
		t.Fatalf("Record after an unrecoverable failure = %v", err)
	}
}

func TestRecordAfterClose(t *testing.T) {
	l := open(t, Options{Path: filepath.Join(t.TempDir(), "d.jsonl")})
	l.Close()
	if err := l.Record(entry(0)); !errors.Is(err, ErrClosed) || ErrClass(err) != "closed" {
		t.Fatalf("Record after Close = %v", err)
	}
}

func TestInvalidEntryIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	if err := l.Record(Entry{RequestID: NewID(), Adapter: "codex", Profile: "x", Decision: "maybe"}); ErrClass(err) != "invalid" {
		t.Fatalf("Record = %v", err)
	}
	write(t, l, 1)
	l.Close()
	verifyOK(t, path, 1)
}

func TestFind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path, MaxBytes: 1200})
	var want ID
	for i := range 12 {
		e := entry(i)
		if i == 7 {
			want = e.RequestID
		}
		if err := l.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	got, err := Find(path, want)
	if err != nil || len(got) != 1 || got[0].Record.RequestID != want.String() || got[0].Line < 1 {
		t.Fatalf("Find = %+v, %v", got, err)
	}
	if got, err := Find(path, NewID()); err != nil || len(got) != 0 {
		t.Fatalf("Find unknown = %+v, %v", got, err)
	}
	if _, err := Find(filepath.Join(t.TempDir(), "none.jsonl"), want); !errors.Is(err, ErrNoLog) {
		t.Fatalf("Find without a log = %v", err)
	}
}

func TestVerifyWithoutALog(t *testing.T) {
	if _, err := Verify(filepath.Join(t.TempDir(), "none.jsonl")); !errors.Is(err, ErrNoLog) {
		t.Fatalf("Verify = %v", err)
	}
}

func TestDiscard(t *testing.T) {
	if err := Discard.Record(Entry{}); err != nil {
		t.Fatal(err)
	}
}
