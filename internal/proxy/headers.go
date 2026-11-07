package proxy

import (
	"net/http"
	"net/url"
	"strings"

	"cui-proxy/internal/transform"
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
	h.Del("Expect") // the body is already buffered; 100-continue is per hop
	h.Set("Accept-Encoding", "identity")
	if _, ok := h["User-Agent"]; !ok {
		h["User-Agent"] = []string{""} // keep Go's default User-Agent out
	}
	return h
}

// credentialHeaders are forwarded untouched and never scanned: their values
// are tokens (a bearer token is itself a "secret" detection).
var credentialHeaders = map[string]bool{
	"Authorization": true, "Proxy-Authorization": true, "Cookie": true, "Chatgpt-Account-Id": true,
}

// requestMetaSensitive reports whether any request header name, any value of
// a non-credential header, or the (percent-decoded) query string contains a
// value the detectors find. Headers and query are never rewritten.
func requestMetaSensitive(r *http.Request, s *transform.Sanitizer) bool {
	for k, vs := range r.Header {
		if len(s.Detect(k)) > 0 {
			return true
		}
		if credentialHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			if len(s.Detect(v)) > 0 {
				return true
			}
		}
	}
	return querySensitive(r.URL.RawQuery, s)
}

// querySensitive scans the raw query, its strict decode when that succeeds,
// and every component split on '&', ';' and '=', each both raw and
// tolerantly decoded. A decode failure never skips a scan, and is not by
// itself a reason to block. Decoding is applied once (no double decoding).
func querySensitive(q string, s *transform.Sanitizer) bool {
	if q == "" {
		return false
	}
	hit := func(t string) bool { return len(s.Detect(t)) > 0 }
	if hit(q) {
		return true
	}
	if d, err := url.QueryUnescape(q); err == nil && d != q && hit(d) {
		return true
	}
	for _, c := range strings.FieldsFunc(q, func(r rune) bool { return r == '&' || r == ';' || r == '=' }) {
		if hit(c) {
			return true
		}
		if d := tolerantUnescape(c); d != c && hit(d) {
			return true
		}
	}
	return false
}

// tolerantUnescape decodes valid %XX escapes and '+' (as a space); invalid
// escapes are kept literally.
func tolerantUnescape(c string) string {
	if !strings.ContainsAny(c, "%+") {
		return c
	}
	b := make([]byte, 0, len(c))
	for i := 0; i < len(c); i++ {
		switch {
		case c[i] == '+':
			b = append(b, ' ')
		case c[i] == '%' && i+2 < len(c) && isHex(c[i+1]) && isHex(c[i+2]):
			b = append(b, unhex(c[i+1])<<4|unhex(c[i+2]))
			i += 2
		default:
			b = append(b, c[i])
		}
	}
	return string(b)
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c <= 'F':
		return c - 'A' + 10
	}
	return c - 'a' + 10
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
