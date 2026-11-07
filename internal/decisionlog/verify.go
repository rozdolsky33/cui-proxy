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
	"strings"
	"time"
)

// maxLine caps a line read back; a longer line is damage, not a record.
const maxLine = 64 << 10

// Break kinds: a closed set.
const (
	BreakMalformed = "malformed"          // not a canonical record line
	BreakHash      = "hash_mismatch"      // the record does not hash to its hash field (edited)
	BreakPrev      = "prev_hash_mismatch" // prev_hash is not the previous record's hash (removed, inserted or reordered)
	BreakTruncated = "truncated"          // the last line has no newline (cut mid-line)
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
	Start     string // prev_hash of the first record: Genesis, or the trusted start after older files were deleted
	StartFile string
	Breaks    []Break
	Gaps      []Gap
	// Unverified counts lines after a break that could not be joined back to
	// the chain.
	Unverified int
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
}

// parseLine decodes and fully checks one line (without its newline): strict
// JSON, every field valid, and byte-for-byte the canonical encoding. It does
// not check the hash.
func parseLine(b []byte) (Record, error) {
	var r Record
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
		v.fail(file, line, BreakTruncated)
		return
	}
	r, err := parseLine(b)
	if err != nil {
		v.fail(file, line, BreakMalformed)
		return
	}
	if hashOf(r.PrevHash, r.canonical()) != r.Hash {
		v.fail(file, line, BreakHash)
		return
	}
	switch {
	case !v.started:
		v.started = true
		v.rep.Start, v.rep.StartFile = r.PrevHash, file
	case r.PrevHash == v.lastHash:
		if v.broken {
			v.rep.Gaps = append(v.rep.Gaps, Gap{v.lastFile, v.lastLine, file, line, v.skipped})
			v.broken = false
		}
	default:
		v.fail(file, line, BreakPrev)
		return
	}
	v.lastHash, v.lastFile, v.lastLine = r.Hash, file, line
	v.rep.Records++
	v.rep.Head, v.rep.HeadFile, v.rep.HeadLine = r.Hash, file, line
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

// readLine reads one line of at most maxLine bytes. A longer line is
// consumed to its end and reported as tooLong.
func readLine(br *bufio.Reader) (line []byte, terminated, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, e := br.ReadSlice('\n')
		if len(buf)+len(chunk) > maxLine+1 {
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

// rotatedLayout is the UTC timestamp suffix of a rotated file:
// <name>.<rotatedLayout>. It sorts in time order as text.
const rotatedLayout = "20060102T150405.000000000Z"

// rotatedFile is a rotated sibling of the active file.
type rotatedFile struct {
	path string
	at   time.Time
}

// rotatedFiles lists the rotated siblings of path, oldest first. Other files
// in the directory are ignored.
func rotatedFiles(path string) ([]rotatedFile, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []rotatedFile
	for _, e := range ents {
		stamp, ok := strings.CutPrefix(e.Name(), base+".")
		if !ok || !e.Type().IsRegular() {
			continue
		}
		t, err := time.Parse(rotatedLayout, stamp)
		if err != nil || t.Format(rotatedLayout) != stamp {
			continue
		}
		out = append(out, rotatedFile{filepath.Join(dir, e.Name()), t})
	}
	slices.SortFunc(out, func(a, b rotatedFile) int { return a.at.Compare(b.at) })
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

// ErrNoLog is returned when neither the active file nor a rotated file exists.
var ErrNoLog = errors.New("no decision log found")

// Verify checks the whole log at path (rotated files and the active file) as
// one chain. The first record of the oldest surviving file is the trusted
// start (Report.Start): Genesis, or the last hash of a file that retention
// deleted. A read error (a file vanished or is unreadable) is returned with
// the partial report.
func Verify(path string) (*Report, error) {
	files, err := Files(path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, ErrNoLog
	}
	v := &verifier{}
	for _, f := range files {
		if err := verifyFile(v, f); err != nil {
			return v.finish(), err
		}
	}
	return v.finish(), nil
}

func verifyFile(v *verifier, path string) error {
	f, err := openRead(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return v.feed(path, f)
}

// Found is a record located by Find.
type Found struct {
	Record Record
	File   string
	Line   int
}

// Find returns every record whose request_id is id, in chain order. Lines
// that do not parse are skipped (Verify reports them).
func Find(path string, id ID) ([]Found, error) {
	files, err := Files(path)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, ErrNoLog
	}
	want := id.String()
	var out []Found
	for _, p := range files {
		f, err := openRead(p)
		if err != nil {
			return out, err
		}
		br := bufio.NewReaderSize(f, 4096)
		n := 0
		for {
			b, terminated, tooLong, err := readLine(br)
			if len(b) > 0 || terminated || tooLong {
				n++
				if terminated && !tooLong && bytes.Contains(b, []byte(want)) {
					if r, perr := parseLine(b); perr == nil && r.RequestID == want {
						out = append(out, Found{r, p, n})
					}
				}
			}
			if err != nil {
				f.Close()
				if !errors.Is(err, io.EOF) {
					return out, err
				}
				break
			}
		}
	}
	return out, nil
}
