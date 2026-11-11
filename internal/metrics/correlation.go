package metrics

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"cui-proxy/internal/observe"
	"encoding/hex"
	"net/http"
	"time"
)

// Identity contains only process-local pseudonyms. Empty fields are unattributed.
type Identity struct {
	Session   string `json:"session,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}
type RecentRequest struct {
	Time                time.Time          `json:"time"`
	Identity            Identity           `json:"identity"`
	WorkspaceAttributed bool               `json:"workspace_attributed"`
	StagesMS            map[string]float64 `json:"stages_ms"`
}
type correlation struct {
	key        [32]byte
	enabled    bool
	generation uint64
	recent     []RecentRequest
}

func (r *Registry) initCorrelation() {
	if _, err := rand.Read(r.correlation.key[:]); err != nil {
		panic("metrics: process randomness unavailable")
	}
}
func (r *Registry) SetCorrelationEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.correlation.enabled != enabled {
		r.correlation.enabled = enabled
		r.correlation.generation++
		r.correlation.recent = nil
	}
}

// CorrelationToken prevents requests begun before a disable/re-enable from repopulating history.
type CorrelationToken struct {
	Identity   Identity
	generation uint64
}

func (r *Registry) Correlate(h http.Header) CorrelationToken {
	r.mu.Lock()
	defer r.mu.Unlock()
	return CorrelationToken{r.identity(h), r.correlation.generation}
}
func (r *Registry) Identity(h http.Header) Identity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.identity(h)
}
func (r *Registry) identity(h http.Header) Identity {
	if !r.correlation.enabled {
		return Identity{}
	}
	return Identity{r.pseudonym(h, "session_id", "session"), r.pseudonym(h, "X-Cui-Workspace-ID", "workspace")}
}
func (r *Registry) pseudonym(h http.Header, header, domain string) string {
	values := h.Values(header)
	if len(values) != 1 {
		return ""
	}
	v := values[0]
	if len(v) < 1 || len(v) > 128 {
		return ""
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return ""
		}
	}
	mac := hmac.New(sha256.New, r.correlation.key[:])
	mac.Write([]byte(domain))
	mac.Write([]byte{0})
	mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}
func (r *Registry) ObserveRecent(id Identity, s observe.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observeRecent(id, s)
}
func (r *Registry) ObserveCorrelated(token CorrelationToken, s observe.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token.generation != r.correlation.generation {
		return
	}
	r.observeRecent(token.Identity, s)
}
func (r *Registry) observeRecent(id Identity, s observe.Snapshot) {
	if !r.correlation.enabled || id.Session == "" && id.Workspace == "" {
		return
	}
	stages := make(map[string]float64)
	for i, seen := range s.Observed {
		if seen {
			stages[observe.Names[i]] = float64(s.Duration[i]) / float64(time.Millisecond)
		}
	}
	r.pruneRecent()
	if len(r.correlation.recent) == 256 {
		copy(r.correlation.recent, r.correlation.recent[1:])
		r.correlation.recent = r.correlation.recent[:255]
	}
	r.correlation.recent = append(r.correlation.recent, RecentRequest{r.now(), id, id.Workspace != "", stages})
}
func (r *Registry) pruneRecent() {
	cutoff := r.now().Add(-15 * time.Minute)
	n := 0
	for _, v := range r.correlation.recent {
		if v.Time.After(cutoff) {
			r.correlation.recent[n] = v
			n++
		}
	}
	clear(r.correlation.recent[n:])
	r.correlation.recent = r.correlation.recent[:n]
}
func (r *Registry) snapshotRecent(s *Snapshot) {
	r.pruneRecent()
	if !r.correlation.enabled {
		return
	}
	for _, v := range r.correlation.recent {
		stages := make(map[string]float64, len(v.StagesMS))
		for k, n := range v.StagesMS {
			stages[k] = n
		}
		v.StagesMS = stages
		s.RecentRequests = append(s.RecentRequests, v)
	}
}
