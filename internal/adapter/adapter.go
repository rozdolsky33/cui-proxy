// Package adapter defines how a harness's wire format is sanitized and
// restored: which routes exist, which request strings are scanned, and which
// response stream fields are restored. Implementations live in subpackages
// (codex) and register themselves with Register.
package adapter

import (
	"errors"
	"net/http"

	"cui-proxy/internal/sse"
	"cui-proxy/internal/transform"
)

// Errors an adapter's SanitizeRequest wraps so the proxy can name the block
// kind without knowing the adapter. Their text is fixed.
var (
	// ErrBadBody: the body is not acceptable (empty, not valid UTF-8, not a
	// single JSON object, a binary part, ...). Block.
	ErrBadBody = errors.New("adapter: request body rejected")
	// ErrDepth: the body nests deeper than the adapter's limit. Block.
	ErrDepth = errors.New("adapter: JSON nested too deeply")
)

// Route kinds.
const (
	// KindSanitize: the request body is scanned and rewritten by SanitizeRequest.
	KindSanitize = "sanitize"
	// KindPassthrough: the request carries no user content (e.g. GET /v1/models)
	// and is forwarded as is.
	KindPassthrough = "passthrough"
)

// Route is a matched request route.
type Route struct {
	Kind         string // KindSanitize or KindPassthrough
	UpstreamPath string // path appended to the upstream base URL
}

// ResponseRewriter restores one response stream. It is not safe for
// concurrent use; create one per response.
type ResponseRewriter interface {
	// Event returns the events to send in place of e. It may return zero
	// events while text is held back. An error means the stream must be cut.
	Event(e sse.Event) ([]sse.Event, error)
	// Flush returns events carrying any text still held back, at end of stream.
	Flush() []sse.Event
}

// Adapter handles one harness's wire format.
type Adapter interface {
	Name() string
	// Match maps a request method and path (no query) to a route. false means
	// the route is unknown and the request must be blocked.
	Match(method, path string) (Route, bool)
	// SanitizeRequest returns the body with every sensitive value replaced.
	// An error means the request must be blocked; the bytes are then nil.
	SanitizeRequest(body []byte, s *transform.Sanitizer) ([]byte, error)
	// NewResponseRewriter returns a rewriter for one response stream.
	NewResponseRewriter(r *transform.Restorer) ResponseRewriter
}

// HeaderSanitizer optionally handles structured adapter-specific metadata.
// Implementations must fail closed and must not alter authentication headers.
type HeaderSanitizer interface {
	SanitizeHeaders(http.Header, *transform.Sanitizer) error
}
