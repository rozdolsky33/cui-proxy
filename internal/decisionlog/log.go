package decisionlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Errors a caller may want to tell apart. Messages carry no record content.
var (
	ErrClosed    = errors.New("decision log is closed")
	ErrSymlink   = errors.New("decision log path is a symlink (refused)")
	ErrInUse     = errors.New("decision log is in use by another process")
	ErrDamaged   = errors.New("decision log is damaged")
	ErrUnsafeDir = errors.New("decision log directory is not private")
)

// Recorder records one decision. The proxy calls it before forwarding.
type Recorder interface {
	Record(Entry) error
}

// Discard is the Recorder used when audit.enabled is false: it records
// nothing and never fails.
var Discard Recorder = discard{}

type discard struct{}

func (discard) Record(Entry) error { return nil }

// DefaultMaxBytes is the rotation size when Options.MaxBytes is not positive.
const DefaultMaxBytes = 50 << 20

// Options configure Open.
type Options struct {
	Path string
	// MaxBytes: before a record would make the file larger than this, the
	// file is rotated to <Path>.<seq>.<UTC timestamp>. <=0 means
	// DefaultMaxBytes.
	MaxBytes int64
	// RetentionDays: rotated files older than this (by the timestamp in
	// their name) are deleted at Open and after each rotation, oldest first,
	// never the newest rotated file, and not at all while the clock reads
	// earlier than the newest record. Each deletion is noted first in
	// <Path>.retention. 0 keeps them forever.
	RetentionDays int
	// NewSegmentOnDamage: when the active file is damaged (a corrupt line, a
	// cut-off last line, a broken chain), move it aside as a rotated file and
	// start a new file whose first record continues from the last good
	// record, instead of refusing to open. A cut head of the log (BreakHeadCut)
	// is only logged then. Verify still reports the damage.
	NewSegmentOnDamage bool
	// Logf receives value-free WARN lines (damage, retention). nil discards
	// them.
	Logf func(format string, args ...any)

	now      func() time.Time                                  // test clock
	openFile func(path string, excl bool) (file, int64, error) // test seam; nil: openAppend
	syncDir  func(dir string)                                  // test seam; nil: syncDir
}

// file is the part of *os.File the writer uses (a test seam).
type file interface {
	io.Writer
	Sync() error
	Truncate(size int64) error
	Close() error
}

// Log appends records to a hash-chained file. Safe for concurrent use: one
// record is one Write and one Sync under a mutex.
type Log struct {
	opts    Options
	now     func() time.Time
	dir     string
	syncDir func(string)

	mu     sync.Mutex
	lock   *os.File // <Path>.lock, flock'ed for the Log's lifetime
	f      file
	size   int64
	prev   string    // hash of the last record written
	lastTS time.Time // ts of the last record (the retention clock check)
	broken error     // set when the file may hold a partial line; every later Record fails
	closed bool
	// openFile opens the active file (a test seam).
	openFile func(path string, excl bool) (file, int64, error)
}

// Open opens (creating if needed) the log at o.Path, checks it, continues its
// chain, and applies retention. The directory is created 0700 and must be a
// real directory owned by this user and not writable by group or others; the
// file is 0600; a symlink at the path is refused. A lock file <Path>.lock is
// held until Close, so a second writer is refused even mid-rotation.
func Open(o Options) (*Log, error) {
	if o.Path == "" {
		return nil, errors.New("decision log: no file configured (audit.file)")
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	l := &Log{opts: o, now: o.now, dir: filepath.Dir(o.Path), syncDir: o.syncDir, openFile: o.openFile}
	if l.now == nil {
		l.now = time.Now
	}
	if l.syncDir == nil {
		l.syncDir = syncDir
	}
	if l.openFile == nil {
		l.openFile = openAppend
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return nil, fmt.Errorf("decision log: %w", err)
	}
	if err := CheckDir(l.dir); err != nil {
		return nil, err
	}
	lock, err := takeLock(lockPath(o.Path))
	if err != nil {
		return nil, err
	}
	l.lock = lock
	if err := l.openActive(); err != nil {
		lock.Close()
		return nil, err
	}
	l.sweep()
	return l, nil
}

// CheckDir refuses a log directory that someone else could change: a
// symlink, not a directory, owned by another user, or writable by group or
// others. (Planted or deleted files there would forge or hide records.) It
// only looks (Lstat).
func CheckDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("decision log: %w", err)
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: %s is a symlink", ErrUnsafeDir, dir)
	case !fi.IsDir():
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeDir, dir)
	case fi.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("%w: %s is writable by group or others (chmod 700 it, or choose another audit.file)", ErrUnsafeDir, dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%w: %s is owned by another user", ErrUnsafeDir, dir)
	}
	return nil
}

// ownedByMe checks an opened file's owner.
func ownedByMe(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Getuid()
}

// takeLock opens (creating 0600) and exclusively locks the lock file.
func takeLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
			return nil, fmt.Errorf("%w: %s", ErrSymlink, path)
		}
		return nil, fmt.Errorf("decision log: %w", err)
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || !ownedByMe(fi) {
		f.Close()
		return nil, fmt.Errorf("%w: %s is not a regular file of this user", ErrUnsafeDir, path)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: %s", ErrInUse, path)
	}
	return f, nil
}

// openActive opens the active file and finds where its chain continues:
// after its own last record, or, when it is empty, after the newest rotated
// file's last good record. It also checks the start of the whole log.
func (l *Log) openActive() error {
	path := l.opts.Path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, path)
	}
	f, size, err := l.openFile(path, false)
	if err != nil {
		return err
	}
	l.syncDir(l.dir) // the file may have just been created
	rs, err := rotatedFiles(path)
	if err != nil {
		f.Close()
		return fmt.Errorf("decision log: %w", err)
	}
	prev, err := predecessorHead(rs)
	if err != nil {
		f.Close()
		return fmt.Errorf("decision log: %w", err)
	}
	rep, err := scanActive(path)
	if err != nil {
		f.Close()
		return err
	}
	if rep.HeadTS != "" {
		l.lastTS, _ = time.Parse(time.RFC3339Nano, rep.HeadTS)
	}
	if err := l.checkHead(rs, rep); err != nil {
		f.Close()
		return err
	}
	// Damage inside the file, or a first record that does not continue the
	// previous file (unless no previous file survives: then checkHead decides).
	linkBroken := rep.Records > 0 && len(rs) > 0 && rep.Start != prev
	if rep.OK() && !linkBroken {
		if rep.Records > 0 {
			prev = rep.Head
		}
		l.f, l.size, l.prev = f, size, prev
		return nil
	}
	where := "its first record, which does not continue the previous file"
	if len(rep.Breaks) > 0 {
		where = fmt.Sprintf("line %d (%s)", rep.Breaks[0].Line, rep.Breaks[0].Kind)
	}
	if !l.opts.NewSegmentOnDamage {
		f.Close()
		return fmt.Errorf("%w: %s at %s; run `cui-proxy audit verify -file %s`, then move the file aside to start a new chain", ErrDamaged, path, where, path)
	}
	// Continue from the last record that verified (the chain head as Verify
	// will see it), so the new segment joins the chain and the damage shows
	// as a break and a gap rather than a second, unrelated chain.
	good := prev
	if rep.Records > 0 {
		good = rep.Head
	}
	rotated, err := l.rotateName(rs)
	if err == nil {
		err = os.Rename(path, rotated)
	}
	f.Close()
	if err != nil {
		return fmt.Errorf("decision log: move the damaged file aside: %w", err)
	}
	l.opts.Logf("WARN decision log damaged at %s: moved it to %s and started a new segment; `cui-proxy audit verify` reports the gap", where, filepath.Base(rotated))
	if f, size, err = l.openFile(path, true); err != nil {
		return err
	}
	l.syncDir(l.dir)
	l.f, l.size, l.prev = f, size, good
	return nil
}

// checkHead checks that the log starts a chain: the first record of the
// oldest file has prev_hash Genesis, or a retention note says retention
// deleted the file it continues. A cut head refuses to open, or (new-segment
// mode) is logged; there is nothing to move aside, the damage is history.
func (l *Log) checkHead(rs []rotatedFile, active *Report) error {
	start, where := "", l.opts.Path
	if len(rs) > 0 {
		where = rs[0].path
		var err error
		if start, err = firstPrev(where); err != nil {
			return fmt.Errorf("decision log: %w", err)
		}
	} else if active.Records > 0 {
		start = active.Start
	}
	if start == "" || start == Genesis {
		return nil
	}
	notes, err := readNotes(l.opts.Path)
	if err != nil {
		return fmt.Errorf("decision log: %w", err)
	}
	if _, ok := notes[start]; ok {
		return nil
	}
	if !l.opts.NewSegmentOnDamage {
		return fmt.Errorf("%w: %s does not start the chain and no retention note explains it (records were removed from the start of the log); run `cui-proxy audit verify -file %s`, then move the files aside to start a new chain", ErrDamaged, where, l.opts.Path)
	}
	l.opts.Logf("WARN decision log: %s does not start the chain and no retention note explains it; `cui-proxy audit verify` reports it", filepath.Base(where))
	return nil
}

// firstPrev is the prev_hash of a file's first line, or "" when it has no
// readable first record.
func firstPrev(path string) (string, error) {
	f, err := openRead(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, terminated, tooLong, err := readLine(bufio.NewReaderSize(f, 4096))
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if !terminated || tooLong {
		return "", nil
	}
	r, perr := parseLine(b)
	if perr != nil {
		return "", nil
	}
	return r.PrevHash, nil
}

// scanActive verifies the active file alone (its first prev_hash trusted).
func scanActive(path string) (*Report, error) {
	f, err := openRead(path)
	if err != nil {
		return nil, fmt.Errorf("decision log: %w", err)
	}
	defer f.Close()
	rep, err := VerifyReader(path, f)
	if err != nil {
		return nil, fmt.Errorf("decision log: read %s: %w", path, err)
	}
	return rep, nil
}

// fileHead is the hash of the last good record of path, or Genesis.
func fileHead(path string) (string, error) {
	f, err := openRead(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	rep, err := VerifyReader(path, f)
	if err != nil {
		return "", err
	}
	if rep.Records == 0 {
		return Genesis, nil
	}
	return rep.Head, nil
}

// predecessorHead is the head of the newest rotated file (by sequence, not
// by clock), or Genesis when there is none.
func predecessorHead(rs []rotatedFile) (string, error) {
	if len(rs) == 0 {
		return Genesis, nil
	}
	return fileHead(rs[len(rs)-1].path)
}

// openAppend opens path for appending without following a symlink, checks
// it is a regular file of this user, and makes it 0600. excl requires the
// file to be new. (The lock is the separate lock file.)
func openAppend(path string, excl bool) (file, int64, error) {
	flags := os.O_WRONLY | os.O_APPEND | os.O_CREATE | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	if excl {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
			return nil, 0, fmt.Errorf("%w: %s", ErrSymlink, path)
		}
		return nil, 0, fmt.Errorf("decision log: %w", err)
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, 0, fmt.Errorf("decision log: %s is not a regular file", path)
	}
	if !ownedByMe(fi) {
		f.Close()
		return nil, 0, fmt.Errorf("%w: %s is owned by another user", ErrUnsafeDir, path)
	}
	if fi.Mode().Perm() != 0o600 {
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return nil, 0, fmt.Errorf("decision log: %w", err)
		}
	}
	return f, fi.Size(), nil
}

// openRead opens path for reading without following a symlink.
func openRead(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
			return nil, fmt.Errorf("%w: %s", ErrSymlink, path)
		}
		return nil, err
	}
	return f, nil
}

// syncDir makes a create, rename or remove in dir durable.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

// Record appends one record and syncs it to disk before returning. On any
// failure nothing of the record stays in the file (it is truncated back);
// if even that fails, the log refuses every later record.
func (l *Log) Record(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	if l.broken != nil {
		return l.broken
	}
	now := l.now()
	r, err := newRecord(e, now)
	if err != nil {
		return err
	}
	r.seal(l.prev)
	line := r.line()
	if len(line)-1 > MaxLine { // cannot happen with MaxRules; never write what Verify would reject
		return fmt.Errorf("%w: record too long", errInvalid)
	}
	if l.size > 0 && l.size+int64(len(line)) > l.opts.MaxBytes {
		if err := l.rotate(); err != nil {
			return err
		}
	}
	if err := l.append(line); err != nil {
		return err
	}
	l.prev, l.lastTS = r.Hash, now
	return nil
}

// append writes and syncs line; on failure it truncates back to l.size.
func (l *Log) append(line []byte) error {
	n, err := l.f.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = l.f.Sync()
	}
	if err != nil {
		if terr := l.f.Truncate(l.size); terr != nil {
			l.broken = fmt.Errorf("decision log: a failed write could not be undone; restart required: %w", err)
		}
		return fmt.Errorf("decision log: write: %w", err)
	}
	l.size += int64(len(line))
	return nil
}

// rotateName is <path>.<next seq>.<UTC timestamp>, the sequence one more
// than the highest in rs.
func (l *Log) rotateName(rs []rotatedFile) (string, error) {
	seq := 1
	for _, r := range rs {
		if r.seq >= seq {
			seq = r.seq + 1
		}
	}
	p := rotatedName(l.opts.Path, seq, l.now())
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("no free name for a rotated file")
	}
	return p, nil
}

// rotate moves the active file aside and starts a new one; the chain
// continues (the new file's first prev_hash is l.prev). The lock file is held
// throughout, so no other writer can take over. On failure the log is
// broken: there may be no active file.
func (l *Log) rotate() error {
	rs, err := rotatedFiles(l.opts.Path)
	if err != nil {
		return fmt.Errorf("decision log: rotate: %w", err)
	}
	name, err := l.rotateName(rs)
	if err != nil {
		return fmt.Errorf("decision log: rotate: %w", err)
	}
	if err := os.Rename(l.opts.Path, name); err != nil {
		return fmt.Errorf("decision log: rotate: %w", err)
	}
	old := l.f
	f, size, err := l.openFile(l.opts.Path, true)
	if err != nil {
		l.broken = fmt.Errorf("decision log: rotate: restart required: %w", err)
		return l.broken
	}
	l.syncDir(l.dir) // the rename and the new file, before its first record is acknowledged
	old.Close()
	l.f, l.size = f, size
	l.sweep()
	return nil
}

// sweep applies retention. It deletes rotated files whose name timestamp is
// older than the retention period, oldest first, and stops at the first file
// that does not qualify, so the chain never gets a hole. It never deletes
// the newest rotated file, never a file named in the future, and nothing at
// all while the clock reads earlier than the newest rotated file or the last
// record (a clock that stepped back). Each deletion is noted, synced, in
// <Path>.retention first, so Verify can tell retention from a cut.
func (l *Log) sweep() {
	days := l.opts.RetentionDays
	if days <= 0 {
		return
	}
	rs, err := rotatedFiles(l.opts.Path)
	if err != nil {
		l.opts.Logf("WARN decision log retention: cannot list rotated files (%s)", ErrClass(err))
		return
	}
	if len(rs) < 2 {
		return
	}
	now := l.now()
	newest := l.lastTS
	for _, r := range rs {
		if r.at.After(newest) {
			newest = r.at
		}
	}
	if now.Before(newest) {
		l.opts.Logf("WARN decision log retention skipped: the clock reads earlier than the newest record or rotated file")
		return
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	for _, r := range rs[:len(rs)-1] {
		if !r.at.Before(cutoff) {
			return
		}
		head, err := fileHead(r.path)
		if err == nil {
			err = l.note(RetentionNote{TS: now.UTC().Format(time.RFC3339Nano), File: filepath.Base(r.path), Head: head})
		}
		if err == nil {
			err = os.Remove(r.path)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			l.opts.Logf("WARN decision log retention: cannot delete %s (%s)", filepath.Base(r.path), ErrClass(err))
			return
		}
		l.syncDir(l.dir)
	}
}

// note appends one retention note and syncs it.
func (l *Log) note(n RetentionNote) error {
	b, err := json.Marshal(n)
	if err != nil {
		return err
	}
	f, _, err := openAppend(notesPath(l.opts.Path), false)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Close closes the active file and releases the lock. Later Records return
// ErrClosed.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	var err error
	if l.f != nil {
		err = l.f.Close()
	}
	if l.lock != nil {
		l.lock.Close()
	}
	return err
}

// ErrClass names an error's category in a closed set, for diagnostics that
// must not carry paths or other text: no_space, permission, symlink, in_use,
// closed, invalid, damaged, unsafe_dir, io.
func ErrClass(err error) string {
	switch {
	case errors.Is(err, ErrClosed):
		return "closed"
	case errors.Is(err, ErrSymlink):
		return "symlink"
	case errors.Is(err, ErrInUse):
		return "in_use"
	case errors.Is(err, ErrDamaged):
		return "damaged"
	case errors.Is(err, ErrUnsafeDir):
		return "unsafe_dir"
	case errors.Is(err, errInvalid):
		return "invalid"
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "no_space"
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EROFS):
		return "permission"
	}
	return "io"
}
