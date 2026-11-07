package decisionlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Break kinds: a closed set.
const (
	BreakMalformed = "malformed"          // not a canonical record line
	BreakHash      = "hash_mismatch"      // the record does not hash to its hash field (edited)
	BreakPrev      = "prev_hash_mismatch" // prev_hash is not the previous record's hash (removed, inserted or reordered)
	BreakTruncated = "truncated"          // the last line has no newline (cut mid-line)
	// BreakHeadCut: the first surviving record does not start a chain (its
	// prev_hash is not Genesis) and no retention note says that retention
	// deleted the file it continues. Lines or files were removed from the
	// start of the log.
	BreakHeadCut = "head_cut"
)

// Break is the first bad line of a damaged region.
type Break struct {
	File string
	Line int
	Kind string
}

func (b Break) String() string { return fmt.Sprintf("%s line %d: %s", b.File, b.Line, b.Kind) }

// Gap is where the chain resumes after damage: the record at File:Line
// continues from the last good record at AfterFile:AfterLine, and the lines
// between are not part of the chain.
type Gap struct {
	AfterFile string
	AfterLine int
	File      string
	Line      int
	Skipped   int // lines in between that could not be verified
}

// Report is the outcome of a verification.
type Report struct {
	Files     []string // in chain order
	Records   int      // records verified as part of the chain
	Head      string   // hash of the last verified record ("" if none)
	HeadFile  string
	HeadLine  int
	HeadTS    string // ts of the last verified record
	Start     string // prev_hash of the first record: Genesis, or a hash retention explains
	StartFile string
	StartLine int
	// StartNote is the retention note that explains a non-Genesis start
	// (Verify only; nil for Genesis or when nothing explains it, which is a
	// BreakHeadCut).
	StartNote *RetentionNote
	Breaks    []Break
	Gaps      []Gap
	// Unverified counts lines after a break that could not be joined back to
	// the chain.
	Unverified int
	// InFlight: the active file ends in a partial line while a live writer
	// holds the log's lock: a record being written, not counted as damage.
	InFlight bool
}

// OK reports whether every line verified and the chain has no gap.
func (r *Report) OK() bool { return len(r.Breaks) == 0 && len(r.Gaps) == 0 && r.Unverified == 0 }

// verifier walks lines in chain order.
type verifier struct {
	rep      Report
	started  bool
	lastHash string // hash of the last good record
	lastFile string
	lastLine int
	broken   bool // inside a damaged region
	skipped  int
	// tailInFlight: an unterminated last line of the file being fed is a
	// record in flight, not damage.
	tailInFlight bool
	// onRecord, if set, sees every line that parses as a record, with the
	// break kind it caused ("" when it verified as part of the chain).
	onRecord func(file string, line int, r Record, kind string)
}

// parseLine decodes and fully checks one line (without its newline): strict
// JSON, every field valid, and byte-for-byte the canonical encoding. It does
// not check the hash.
func parseLine(b []byte) (Record, error) {
	var r Record
	if len(b) > MaxLine {
		return Record{}, fmt.Errorf("%w: %s", errInvalid, "too long")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Record{}, fmt.Errorf("%w: %s", errInvalid, "json")
	}
	if dec.More() {
		return Record{}, fmt.Errorf("%w: %s", errInvalid, "trailing data")
	}
	if r.Counts == nil || r.RuleIDs == nil || r.Hash == "" || r.PrevHash == "" {
		return Record{}, fmt.Errorf("%w: %s", errInvalid, "missing field")
	}
	if err := r.check(); err != nil {
		return Record{}, err
	}
	want := r.line()
	if !bytes.Equal(want[:len(want)-1], b) {
		return Record{}, fmt.Errorf("%w: %s", errInvalid, "not canonical")
	}
	return r, nil
}

func (v *verifier) fail(file string, line int, kind string) {
	if !v.broken {
		v.rep.Breaks = append(v.rep.Breaks, Break{file, line, kind})
		v.broken = true
		v.skipped = 0
	}
	v.skipped++
}

// add verifies one line (without its newline).
func (v *verifier) add(file string, line int, b []byte, terminated bool) {
	if !terminated {
		if v.tailInFlight {
			v.rep.InFlight = true
			return
		}
		v.fail(file, line, BreakTruncated)
		return
	}
	r, err := parseLine(b)
	if err != nil {
		v.fail(file, line, BreakMalformed)
		return
	}
	kind := ""
	defer func() {
		if v.onRecord != nil {
			v.onRecord(file, line, r, kind)
		}
	}()
	if hashOf(r.PrevHash, r.canonical()) != r.Hash {
		kind = BreakHash
		v.fail(file, line, kind)
		return
	}
	switch {
	case !v.started:
		v.started = true
		v.rep.Start, v.rep.StartFile, v.rep.StartLine = r.PrevHash, file, line
	case r.PrevHash == v.lastHash:
		if v.broken {
			v.rep.Gaps = append(v.rep.Gaps, Gap{v.lastFile, v.lastLine, file, line, v.skipped})
			v.broken = false
		}
	default:
		kind = BreakPrev
		v.fail(file, line, kind)
		return
	}
	v.lastHash, v.lastFile, v.lastLine = r.Hash, file, line
	v.rep.Records++
	v.rep.Head, v.rep.HeadFile, v.rep.HeadLine, v.rep.HeadTS = r.Hash, file, line, r.TS
}

// feed verifies every line of rd as part of file name.
func (v *verifier) feed(name string, rd io.Reader) error {
	v.rep.Files = append(v.rep.Files, name)
	br := bufio.NewReaderSize(rd, 4096)
	n := 0
	for {
		b, terminated, tooLong, err := readLine(br)
		if len(b) > 0 || terminated || tooLong {
			n++
			if tooLong {
				v.fail(name, n, BreakMalformed)
			} else {
				v.add(name, n, b, terminated)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// finish closes the walk: a damaged region that runs to the end is counted
// as unverified.
func (v *verifier) finish() *Report {
	if v.broken {
		v.rep.Unverified += v.skipped
		v.broken, v.skipped = false, 0
	}
	// Lines skipped in a region that was later bridged by a gap are counted
	// in the gap; count them as unverified too so OK stays false.
	for _, g := range v.rep.Gaps {
		v.rep.Unverified += g.Skipped
	}
	return &v.rep
}

// readLine reads one line of at most MaxLine bytes. A longer line is
// consumed to its end and reported as tooLong.
func readLine(br *bufio.Reader) (line []byte, terminated, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, e := br.ReadSlice('\n')
		if len(buf)+len(chunk) > MaxLine+1 {
			tooLong = true
			buf = buf[:0]
		} else if !tooLong {
			buf = append(buf, chunk...)
		}
		if errors.Is(e, bufio.ErrBufferFull) {
			continue
		}
		if e == nil {
			return bytes.TrimSuffix(buf, []byte{'\n'}), true, tooLong, nil
		}
		return buf, false, tooLong, e
	}
}

// VerifyReader verifies one stream of lines as a whole chain (the first
// record's prev_hash is trusted). It never panics; the report says what is
// wrong. The error is only a read error.
func VerifyReader(name string, rd io.Reader) (*Report, error) {
	v := &verifier{}
	if err := v.feed(name, rd); err != nil {
		return v.finish(), err
	}
	return v.finish(), nil
}

// Rotated files are named <name>.<seq>.<UTC timestamp>: seq is a 10-digit
// sequence number, one more than the highest in the directory, so the order
// does not depend on the clock. Files named <name>.<timestamp> (the first
// release) are read too, before every numbered file, in timestamp order.
const (
	rotatedLayout = "20060102T150405.000000000Z"
	seqDigits     = 10
)

// rotatedFile is a rotated sibling of the active file.
type rotatedFile struct {
	path string
	seq  int // 0 for an old, unnumbered name
	at   time.Time
}

// parseRotated reads a rotated file name's suffix (after "<name>.").
func parseRotated(suffix string) (seq int, at time.Time, ok bool) {
	if len(suffix) > seqDigits+1 && suffix[seqDigits] == '.' {
		n, err := strconv.Atoi(suffix[:seqDigits])
		if err == nil && n > 0 && strconv.Itoa(n) == strings.TrimLeft(suffix[:seqDigits], "0") {
			seq, suffix = n, suffix[seqDigits+1:]
		}
	}
	t, err := time.Parse(rotatedLayout, suffix)
	if err != nil || t.Format(rotatedLayout) != suffix {
		return 0, time.Time{}, false
	}
	return seq, t, true
}

func rotatedName(path string, seq int, at time.Time) string {
	return fmt.Sprintf("%s.%0*d.%s", path, seqDigits, seq, at.UTC().Format(rotatedLayout))
}

// rotatedFiles lists the rotated siblings of path in chain order. Other
// files (and symlinks) in the directory are ignored.
func rotatedFiles(path string) ([]rotatedFile, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []rotatedFile
	for _, e := range ents {
		suffix, ok := strings.CutPrefix(e.Name(), base+".")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		if seq, at, ok := parseRotated(suffix); ok {
			out = append(out, rotatedFile{filepath.Join(dir, e.Name()), seq, at})
		}
	}
	slices.SortFunc(out, func(a, b rotatedFile) int {
		if a.seq != b.seq {
			return a.seq - b.seq
		}
		return a.at.Compare(b.at)
	})
	return out, nil
}

// Files returns the log's files in chain order: rotated files oldest first,
// then the active file if it exists.
func Files(path string) ([]string, error) {
	rs, err := rotatedFiles(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rs {
		out = append(out, r.path)
	}
	if fi, err := os.Lstat(path); err == nil {
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", path)
		}
		out = append(out, path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

// RetentionNote is one line of <name>.retention: retention deleted File
// (a base name) at TS, and Head was the hash of its last record. It holds
// no record content.
type RetentionNote struct {
	TS   string `json:"ts"`
	File string `json:"file"`
	Head string `json:"head"`
}

func notesPath(path string) string { return path + ".retention" }
func lockPath(path string) string  { return path + ".lock" }

// readNotes reads the retention notes, keyed by head hash. A missing file is
// no notes; lines that do not parse are ignored (they explain nothing).
func readNotes(path string) (map[string]RetentionNote, error) {
	f, err := openRead(notesPath(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]RetentionNote{}
	br := bufio.NewReaderSize(f, 4096)
	for {
		b, terminated, tooLong, err := readLine(br)
		if terminated && !tooLong {
			var n RetentionNote
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.DisallowUnknownFields()
			if dec.Decode(&n) == nil && isHash(n.Head) {
				out[n.Head] = n
			}
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// liveWriter reports whether a process holds the log's lock (serve is
// running). It opens the lock file read-only and never creates it.
func liveWriter(path string) bool {
	f, err := openRead(lockPath(path))
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// ErrNoLog is returned when neither the active file nor a rotated file exists.
var ErrNoLog = errors.New("no decision log found")

// walk feeds every file of the log at path, in chain order, to v, then checks
// the start of the chain against the retention notes.
func walk(path string, v *verifier) (*Report, error) {
	files, err := Files(path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, ErrNoLog
	}
	live := liveWriter(path)
	for _, f := range files {
		v.tailInFlight = live && f == path
		if err := verifyFile(v, f); err != nil {
			return v.finish(), err
		}
	}
	rep := v.finish()
	if rep.Records > 0 && rep.Start != Genesis {
		notes, err := readNotes(path)
		if err != nil {
			return rep, err
		}
		if n, ok := notes[rep.Start]; ok {
			rep.StartNote = &n
		} else {
			rep.Breaks = append([]Break{{rep.StartFile, rep.StartLine, BreakHeadCut}}, rep.Breaks...)
		}
	}
	return rep, nil
}

// Verify checks the whole log at path (rotated files and the active file) as
// one chain. A chain that does not start at Genesis is accepted only when a
// retention note says retention deleted the file it continues (Report.Start
// and StartNote); otherwise it is a BreakHeadCut. A partial last line of the
// active file while serve holds the lock is reported as InFlight, not as
// damage. A read error is returned with the partial report.
func Verify(path string) (*Report, error) {
	return walk(path, &verifier{})
}

func verifyFile(v *verifier, path string) error {
	f, err := openRead(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return v.feed(path, f)
}

// Found is a record located by Find. Problem is "" when the record verified
// as part of the chain, else the break kind it caused (BreakHash: the line
// was edited; BreakPrev: it is out of place). A record before a BreakHeadCut
// start is reported with Problem BreakHeadCut.
type Found struct {
	Record  Record
	File    string
	Line    int
	Problem string
}

// Find returns every record whose request_id is id, in chain order, each
// with the outcome of verifying it in place.
func Find(path string, id ID) ([]Found, error) {
	want := id.String()
	var out []Found
	v := &verifier{onRecord: func(file string, line int, r Record, kind string) {
		if r.RequestID == want {
			out = append(out, Found{r, file, line, kind})
		}
	}}
	rep, err := walk(path, v)
	if rep != nil && len(rep.Breaks) > 0 && rep.Breaks[0].Kind == BreakHeadCut {
		for i := range out {
			if out[i].Problem == "" {
				out[i].Problem = BreakHeadCut
			}
		}
	}
	return out, err
}
