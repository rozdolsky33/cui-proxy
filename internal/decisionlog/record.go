// Package decisionlog is the persisted, value-free, tamper-evident decision
// log (ADR 0001, phase 2): one JSON line per request decision, appended to a
// file and chained by SHA-256, so an edited, reordered or removed line in the
// middle of the log is detected by Verify.
//
// A record is built only from closed types and checked names: a request id
// minted here, a decision and a block reason from closed sets, and adapter,
// profile and rule names cleaned by SafeName. There is no field that could
// hold a value, a header, a prompt, a token, a URL or anything from a request
// or response body.
//
// The chain is tamper-evident, not tamper-proof: without an external anchor,
// someone who can rewrite the file can recompute every hash, and deleting the
// newest lines leaves a shorter chain that still verifies.
//
// The in-memory dashboard feed is a different thing: see package audit.
package decisionlog

import (
	"crypto/rand"
	"crypto/sha256"
	"cui-proxy/internal/attachment"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Decision is what the proxy did with a request: a closed set.
type Decision string

const (
	Forwarded Decision = "forwarded" // sent upstream unchanged (nothing detected, or on_error: passthrough)
	Sanitized Decision = "sanitized" // sent upstream with detected values masked
	Blocked   Decision = "blocked"   // refused; never sent upstream
)

func (d Decision) valid() bool { return d == Forwarded || d == Sanitized || d == Blocked }

// reasons is the closed set of block reasons a record may carry: the proxy's
// block reasons (internal/proxy) plus "audit". A proxy test checks that every
// proxy block reason is in this set.
var reasons = map[string]bool{
	"route": true, "overload": true, "vault": true, "body_too_large": true, "compressed": true, "sanitize": true,
	"unrestorable": true, "bad_request": true, "internal": true, "policy": true,
	"header": true, "audit": true, "client": true,
}

// KnownReason reports whether r is a block reason a record may carry.
func KnownReason(r string) bool { return reasons[r] }

// passthroughPrefix marks the counts key of a request forwarded unsanitized
// because policy.on_error is passthrough: "passthrough:<reason>". The key is
// built only by newRecord, from Entry.Passthrough. Every key that comes from
// Entry.Counts goes through SafeName, which maps ':' to '_', so a rule name
// can never produce it (nor the truncation key below).
const passthroughPrefix = "passthrough:"

// TruncatedKey is the counts key, with the number of rules left out, of a
// record that matched more than MaxRules distinct rules. A record keeps the
// MaxRules rules with the highest counts (ties by name), so the line stays
// far below the verifier's line limit and the decision is still recorded.
const TruncatedKey = "truncated:rules"

// MaxRules caps the rules in one record (see TruncatedKey).
const MaxRules = 64

// MaxLine is the longest line the writer emits and the verifier accepts
// (without its newline). With MaxRules the longest possible record is about
// 10 KiB; a record over MaxLine is refused, never written.
const MaxLine = 64 << 10

// PassthroughKey is the counts key recorded for a request forwarded
// unsanitized under policy.on_error: passthrough after a failure with reason.
func PassthroughKey(reason string) string { return passthroughPrefix + reason }

// ID is a request id: 8 random bytes, printed as 16 lowercase hex digits.
type ID [8]byte

// NewID returns a random request id.
func NewID() ID {
	var id ID
	_, _ = rand.Read(id[:]) // crypto/rand.Read never fails (it crashes the program instead)
	return id
}

func (id ID) String() string { return hex.EncodeToString(id[:]) }

// ParseID parses a 16-digit lowercase hex request id.
func ParseID(s string) (ID, error) {
	var id ID
	if len(s) != 2*len(id) || !isLowerHex(s) {
		return id, errors.New("a request id is 16 lowercase hex digits")
	}
	_, err := hex.Decode(id[:], []byte(s))
	return id, err
}

// maxNameLen caps a name, as in the proxy's diagnostics.
const maxNameLen = 64

// SafeName keeps a config-supplied name printable in a log line, a record and
// a JSON string without escaping: bytes outside [A-Za-z0-9._-] become '_',
// and the result is cut to 64 bytes. The proxy's diagnostics use the same.
func SafeName(s string) string {
	if len(s) > maxNameLen {
		s = s[:maxNameLen]
	}
	b := []byte(s)
	for i, c := range b {
		if !safeByte(c) {
			b[i] = '_'
		}
	}
	return string(b)
}

func safeByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
}

func isSafeName(s string) bool { return s != "" && len(s) <= maxNameLen && SafeName(s) == s }

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Entry is what the proxy hands over for one decision. Every string in it
// is checked or cleaned by Record before anything is written.
type Entry struct {
	RequestID ID
	Adapter   string         // cleaned with SafeName
	Profile   string         // cleaned with SafeName
	Decision  Decision       // closed set
	Reason    string         // a KnownReason when Decision is Blocked, else ""
	Counts    map[string]int // rule name (cleaned with SafeName) -> count
	// Passthrough is set (to the failure's reason) for a request forwarded
	// unsanitized because policy.on_error is passthrough. Decision must be
	// Forwarded. It is the only way to record a passthrough marker.
	Passthrough string
	Detail      string // closed attachment diagnostic, only for sanitize blocks
}

// Record is one line of the log. Field order here is the canonical order.
type Record struct {
	TS         string         `json:"ts"`
	RequestID  string         `json:"request_id"`
	Adapter    string         `json:"adapter"`
	Profile    string         `json:"profile"`
	Decision   Decision       `json:"decision"`
	Reason     string         `json:"reason"`
	RuleIDs    []string       `json:"rule_ids"`
	Counts     map[string]int `json:"counts"`
	Detail     string         `json:"detail,omitempty"`
	ApprovalID string         `json:"approval_id"` // always "" until approvals exist (ADR 0001, phase 3)
	PrevHash   string         `json:"prev_hash"`
	Hash       string         `json:"hash"`
}

// Genesis is the prev_hash of the first record of a new chain.
var Genesis = strings.Repeat("0", 64)

// errInvalid wraps every reason a record is refused or a line is rejected.
var errInvalid = errors.New("invalid record")

// newRecord builds the record for e (without prev_hash and hash), or refuses
// an entry that does not fit the closed shape.
func newRecord(e Entry, now time.Time) (Record, error) {
	r := Record{
		TS:        now.UTC().Format(time.RFC3339Nano),
		RequestID: e.RequestID.String(),
		Adapter:   SafeName(e.Adapter),
		Profile:   SafeName(e.Profile),
		Decision:  e.Decision,
		Reason:    e.Reason,
		Detail:    e.Detail,
		Counts:    map[string]int{},
	}
	for k, n := range e.Counts {
		if n <= 0 {
			return Record{}, fmt.Errorf("%w: a count must be positive", errInvalid)
		}
		r.Counts[SafeName(k)] += n
	}
	capRules(r.Counts)
	if e.Passthrough != "" {
		if !reasons[e.Passthrough] || e.Decision != Forwarded {
			return Record{}, fmt.Errorf("%w: passthrough", errInvalid)
		}
		r.Counts[PassthroughKey(e.Passthrough)] = 1
	}
	r.RuleIDs = ruleIDs(r.Counts)
	if err := r.check(); err != nil {
		return Record{}, err
	}
	return r, nil
}

// capRules keeps at most MaxRules rule keys in counts: the highest counts,
// ties by name, plus TruncatedKey with the number left out.
func capRules(counts map[string]int) {
	if len(counts) <= MaxRules {
		return
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return strings.Compare(a, b)
	})
	for _, k := range keys[MaxRules:] {
		delete(counts, k)
	}
	counts[TruncatedKey] = len(keys) - MaxRules
}

// isPassthroughKey reports whether k is a marker newRecord built from
// Entry.Passthrough (never a cleaned rule name, which has no ':').
func isPassthroughKey(k string) bool {
	rest, ok := strings.CutPrefix(k, passthroughPrefix)
	return ok && reasons[rest]
}

// isRuleKey reports whether a counts key is a rule name (not a marker).
func isRuleKey(k string) bool { return !strings.Contains(k, ":") }

// ruleIDs is the sorted rule names of counts (markers are not rules).
func ruleIDs(counts map[string]int) []string {
	ids := []string{}
	for k := range counts {
		if isRuleKey(k) {
			ids = append(ids, k)
		}
	}
	slices.Sort(ids)
	return ids
}

// check validates every field except the hashes' relationship. It is applied
// both before writing and to every line read back.
func (r Record) check() error {
	bad := func(what string) error { return fmt.Errorf("%w: %s", errInvalid, what) }
	if t, err := time.Parse(time.RFC3339Nano, r.TS); err != nil || t.Location() != time.UTC || t.Format(time.RFC3339Nano) != r.TS {
		return bad("ts")
	}
	if _, err := ParseID(r.RequestID); err != nil {
		return bad("request_id")
	}
	if !isSafeName(r.Adapter) {
		return bad("adapter")
	}
	if !isSafeName(r.Profile) {
		return bad("profile")
	}
	if !r.Decision.valid() {
		return bad("decision")
	}
	if (r.Decision == Blocked) != (r.Reason != "") || (r.Reason != "" && !reasons[r.Reason]) {
		return bad("reason")
	}
	if r.Detail != "" && (!attachment.KnownDetail(r.Detail) || r.Decision != Blocked || r.Reason != "sanitize") {
		return bad("detail")
	}
	for k, n := range r.Counts {
		switch {
		case n <= 0:
			return bad("counts")
		case isSafeName(k):
		case k == TruncatedKey:
		case isPassthroughKey(k) && n == 1 && r.Decision == Forwarded:
		default:
			return bad("counts")
		}
	}
	// A truncation marker only on a record holding exactly MaxRules rules.
	if n := len(ruleIDs(r.Counts)); n > MaxRules || (r.Counts[TruncatedKey] > 0 && n != MaxRules) {
		return bad("counts")
	}
	if !slices.Equal(r.RuleIDs, ruleIDs(r.Counts)) {
		return bad("rule_ids")
	}
	if r.ApprovalID != "" {
		return bad("approval_id")
	}
	// An unsealed record (from newRecord) has neither hash yet.
	if (r.PrevHash != "" || r.Hash != "") && (!isHash(r.PrevHash) || (r.Hash != "" && !isHash(r.Hash))) {
		return bad("hash")
	}
	return nil
}

func isHash(s string) bool { return len(s) == 64 && isLowerHex(s) }

// canonical is the record as JSON with the keys in the fixed order of Record
// and without the hash field. Every string in a checked record is ASCII from
// a set that needs no escaping, so strconv.Quote and encoding/json agree.
func (r Record) canonical() []byte {
	b := make([]byte, 0, 256)
	str := func(key, v string) {
		b = append(b, '"')
		b = append(b, key...)
		b = append(b, `":`...)
		b = strconv.AppendQuote(b, v)
		b = append(b, ',')
	}
	b = append(b, '{')
	str("ts", r.TS)
	str("request_id", r.RequestID)
	str("adapter", r.Adapter)
	str("profile", r.Profile)
	str("decision", string(r.Decision))
	str("reason", r.Reason)
	b = append(b, `"rule_ids":[`...)
	for i, id := range r.RuleIDs {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendQuote(b, id)
	}
	b = append(b, `],"counts":{`...)
	keys := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendQuote(b, k)
		b = append(b, ':')
		b = strconv.AppendInt(b, int64(r.Counts[k]), 10)
	}
	b = append(b, `},`...)
	if r.Detail != "" {
		str("detail", r.Detail)
	}
	str("approval_id", r.ApprovalID)
	b = append(b, `"prev_hash":`...)
	b = strconv.AppendQuote(b, r.PrevHash)
	return append(b, '}')
}

// hashOf is hex(SHA-256(prev_hash bytes || canonical JSON)), where the
// prev_hash bytes are the 32 bytes the hex string decodes to.
func hashOf(prev string, canon []byte) string {
	raw, err := hex.DecodeString(prev)
	if err != nil || len(raw) != sha256.Size {
		panic("decisionlog: hashOf on an unchecked prev_hash")
	}
	h := sha256.New()
	h.Write(raw)
	h.Write(canon)
	return hex.EncodeToString(h.Sum(nil))
}

// line is the record as written: the canonical JSON with the hash appended
// as the last key, and a newline.
func (r Record) line() []byte {
	c := r.canonical()
	out := make([]byte, 0, len(c)+80)
	out = append(out, c[:len(c)-1]...)
	out = append(out, `,"hash":`...)
	out = strconv.AppendQuote(out, r.Hash)
	return append(out, '}', '\n')
}

// seal sets prev_hash and hash.
func (r *Record) seal(prev string) {
	r.PrevHash = prev
	r.Hash = hashOf(prev, r.canonical())
}
