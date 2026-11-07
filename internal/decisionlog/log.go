package decisionlog

import (
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
	ErrClosed  = errors.New("decision log is closed")
	ErrSymlink = errors.New("decision log path is a symlink (refused)")
	ErrInUse   = errors.New("decision log is in use by another process")
	ErrDamaged = errors.New("decision log is damaged")
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
	// file is rotated to <Path>.<UTC timestamp>. <=0 means DefaultMaxBytes.
	MaxBytes int64
	// RetentionDays: rotated files older than this are deleted at Open and
	// after each rotation. 0 keeps them forever.
	RetentionDays int
	// NewSegmentOnDamage: when the active file is damaged (a corrupt line, a
	// cut-off last line, a broken chain), move it aside as a rotated file and
	// start a new file whose first record continues from the last good
	// record, instead of refusing to open. Verify still reports the damage.
	NewSegmentOnDamage bool
	// Logf receives value-free WARN lines (damage, retention failures). nil
	// discards them.
	Logf func(format string, args ...any)

	now      func() time.Time                                  // test clock
	openFile func(path string, excl bool) (file, int64, error) // test seam; nil: openAppend
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
	opts Options
	now  func() time.Time

	mu     sync.Mutex
	f      file
	size   int64
	prev   string // hash of the last record written
	broken error  // set when the file may hold a partial line; every later Record fails
	closed bool
	// openFile opens the active file (a test seam).
	openFile func(path string, excl bool) (file, int64, error)
}

// Open opens (creating if needed) the log at o.Path, checks the active file,
// continues its chain, and applies retention. The directory is created 0700
// and the file 0600; a symlink at the path is refused.
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
	now := o.now
	if now == nil {
		now = time.Now
	}
	l := &Log{opts: o, now: now, openFile: o.openFile}
	if l.openFile == nil {
		l.openFile = openAppend
	}
	if err := os.MkdirAll(filepath.Dir(o.Path), 0o700); err != nil {
		return nil, fmt.Errorf("decision log: %w", err)
	}
	if err := l.openActive(); err != nil {
		return nil, err
	}
	l.sweep()
	return l, nil
}

// openActive opens (and locks) the active file and finds where its chain
// continues: after its own last record, or, when it is empty, after the
// newest rotated file's last good record.
func (l *Log) openActive() error {
	path := l.opts.Path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrSymlink, path)
	}
	f, size, err := l.openFile(path, false) // takes the lock before anything is read
	if err != nil {
		return err
	}
	prev, err := predecessorHead(path)
	if err != nil {
		f.Close()
		return fmt.Errorf("decision log: %w", err)
	}
	rep, err := scanActive(path)
	if err != nil {
		f.Close()
		return err
	}
	// Damage inside the file, or a first record that does not continue the
	// previous file (unless no previous file survives: then it is trusted).
	linkBroken := rep.Records > 0 && prev != Genesis && rep.Start != prev
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
	rotated, err := l.rotateName()
	if err == nil {
		err = os.Rename(path, rotated)
	}
	f.Close()
	if err != nil {
		return fmt.Errorf("decision log: move the damaged file aside: %w", err)
	}
	syncDir(filepath.Dir(path))
	l.opts.Logf("WARN decision log damaged at %s: moved it to %s and started a new segment; `cui-proxy audit verify` reports the gap", where, filepath.Base(rotated))
	if f, size, err = l.openFile(path, true); err != nil {
		return err
	}
	l.f, l.size, l.prev = f, size, good
	return nil
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

// predecessorHead is the hash of the last good record of the newest rotated
// file, or Genesis when there is none.
func predecessorHead(path string) (string, error) {
	rs, err := rotatedFiles(path)
	if err != nil {
		return "", err
	}
	if len(rs) == 0 {
		return Genesis, nil
	}
	f, err := openRead(rs[len(rs)-1].path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	rep, err := VerifyReader(rs[len(rs)-1].path, f)
	if err != nil {
		return "", err
	}
	if rep.Records == 0 {
		return Genesis, nil
	}
	return rep.Head, nil
}

// openAppend opens path for appending without following a symlink, checks
// it is a regular file, makes it 0600, and takes an exclusive lock so two
// processes never interleave one chain. excl requires the file to be new.
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
	if fi.Mode().Perm() != 0o600 {
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return nil, 0, fmt.Errorf("decision log: %w", err)
		}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("%w: %s", ErrInUse, path)
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
	r, err := newRecord(e, l.now())
	if err != nil {
		return err
	}
	r.seal(l.prev)
	line := r.line()
	if l.size > 0 && l.size+int64(len(line)) > l.opts.MaxBytes {
		if err := l.rotate(); err != nil {
			return err
		}
	}
	if err := l.append(line); err != nil {
		return err
	}
	l.prev = r.Hash
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

// rotateName is a free <path>.<UTC timestamp> name.
func (l *Log) rotateName() (string, error) {
	t := l.now().UTC()
	for range 1000 {
		p := l.opts.Path + "." + t.Format(rotatedLayout)
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p, nil
		}
		t = t.Add(time.Nanosecond)
	}
	return "", errors.New("no free name for a rotated file")
}

// rotate moves the active file aside and starts a new one; the chain
// continues (the new file's first prev_hash is l.prev). On failure the log
// is broken: there may be no active file.
func (l *Log) rotate() error {
	name, err := l.rotateName()
	if err != nil {
		return fmt.Errorf("decision log: rotate: %w", err)
	}
	if err := os.Rename(l.opts.Path, name); err != nil {
		return fmt.Errorf("decision log: rotate: %w", err)
	}
	syncDir(filepath.Dir(l.opts.Path))
	old := l.f
	f, size, err := l.openFile(l.opts.Path, true)
	if err != nil {
		l.broken = fmt.Errorf("decision log: rotate: restart required: %w", err)
		return l.broken
	}
	old.Close()
	l.f, l.size = f, size
	l.sweep()
	return nil
}

// sweep deletes rotated files older than the retention period, judged by
// the timestamp in their name. The active file is never touched.
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
	cutoff := l.now().Add(-time.Duration(days) * 24 * time.Hour)
	for _, r := range rs {
		if r.at.Before(cutoff) {
			if err := os.Remove(r.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				l.opts.Logf("WARN decision log retention: cannot delete %s (%s)", filepath.Base(r.path), ErrClass(err))
			}
		}
	}
}

// Close closes the active file. Later Records return ErrClosed.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.f == nil {
		return nil
	}
	return l.f.Close()
}

// ErrClass names an error's category in a closed set, for diagnostics that
// must not carry paths or other text: no_space, permission, symlink, in_use,
// closed, invalid, damaged, io.
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
	case errors.Is(err, errInvalid):
		return "invalid"
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "no_space"
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EROFS):
		return "permission"
	}
	return "io"
}
