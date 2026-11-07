package proxy

import (
	"net/http"
	"strings"
)

// hopByHop are the RFC 9110 section 7.6.1 connection-specific headers plus
// the legacy Keep-Alive and Proxy-Connection. Every other Proxy-* header and
// every header named in Connection are dropped too (see removeHopByHop).
var hopByHop = []string{
	"Connection", "Keep-Alive", "Proxy-Connection", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// removeHopByHop deletes hop-by-hop headers from h in place.
func removeHopByHop(h http.Header) {
	for _, v := range h.Values("Connection") {
		for _, f := range strings.Split(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}
	for k := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(k), "Proxy-") {
			delete(h, k)
		}
	}
	for _, k := range hopByHop {
		h.Del(k)
	}
}

// upstreamHeader returns the headers to send upstream: every end-to-end
// client header untouched (Authorization, Chatgpt-Account-Id, Session-Id,
// X-Codex-*, User-Agent, cookies ...), without hop-by-hop headers and the
// client's Content-Length (the transport sets it from the sanitized body),
// with Accept-Encoding forced to identity so the response can be rewritten.
// No X-Forwarded-* headers are added.
func upstreamHeader(in http.Header) http.Header {
	h := in.Clone()
	if h == nil {
		h = http.Header{}
	}
	removeHopByHop(h)
	h.Del("Content-Length")
	h.Del("Host")
	h.Set("Accept-Encoding", "identity")
	if _, ok := h["User-Agent"]; !ok {
		h["User-Agent"] = []string{""} // keep Go's default User-Agent out
	}
	return h
}

// copyResponseHeader copies the upstream response headers to dst, without
// hop-by-hop headers.
func copyResponseHeader(dst, src http.Header) {
	s := src.Clone()
	removeHopByHop(s)
	for k, v := range s {
		dst[k] = v
	}
}

// encoded reports whether h carries a Content-Encoding other than identity.
// The body could not be scanned, so such a request is blocked.
func encoded(h http.Header) bool {
	for _, v := range h.Values("Content-Encoding") {
		for _, c := range strings.Split(v, ",") {
			if c = strings.TrimSpace(c); c != "" && !strings.EqualFold(c, "identity") {
				return true
			}
		}
	}
	return false
}

// isSSE reports whether the upstream response is an event stream.
func isSSE(h http.Header) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(h.Get("Content-Type"))), "text/event-stream")
}
