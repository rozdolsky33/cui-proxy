// Package codex adapts the OpenAI Responses API traffic of Codex CLI
// (wire_api = "responses"): POST /v1/responses with an SSE response stream,
// and GET /v1/models.
package codex

import (
	"cui-proxy/internal/adapter"
	"cui-proxy/internal/transform"
)

const name = "codex"

func init() {
	if _, ok := adapter.Get(name); !ok {
		adapter.Register(New())
	}
}

// Adapter is the Codex Responses API adapter. It holds no state.
type Adapter struct{}

// New returns the Codex adapter.
func New() *Adapter { return &Adapter{} }

// Name returns "codex".
func (*Adapter) Name() string { return name }

// Match accepts exactly GET /v1/models (passthrough) and POST /v1/responses
// (sanitize); the /v1 prefix is stripped for the upstream path. path carries
// no query; the proxy keeps the query when forwarding.
func (*Adapter) Match(method, path string) (adapter.Route, bool) {
	switch {
	case method == "GET" && path == "/v1/models":
		return adapter.Route{Kind: adapter.KindPassthrough, UpstreamPath: "/models"}, true
	case method == "POST" && path == "/v1/responses":
		return adapter.Route{Kind: adapter.KindSanitize, UpstreamPath: "/responses"}, true
	}
	return adapter.Route{}, false
}

// SanitizeRequest: see request.go.
func (*Adapter) SanitizeRequest(body []byte, s *transform.Sanitizer) ([]byte, error) {
	return sanitizeRequest(body, s)
}

// NewResponseRewriter: see response.go.
func (*Adapter) NewResponseRewriter(r *transform.Restorer) adapter.ResponseRewriter {
	return newRewriter(r)
}

// opaque lists keys whose values are protocol identifiers or opaque blobs.
// They are neither sanitized in requests nor restored in responses, and pass
// byte-identical. Applies to the protocol JSON only, never to JSON documents
// carried inside strings (function-call arguments).
var opaque = map[string]bool{
	"id": true, "call_id": true, "item_id": true, "encrypted_content": true,
	"signature": true, "data": true, "image_url": true, "prompt_cache_key": true,
	"model": true, "type": true, "role": true, "status": true, "name": true,
}
