package codex

import (
	"context"
	"errors"
	"fmt"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/attachment"
	"cui-proxy/internal/transform"
)

// SanitizeRequestWithPolicy uses the request's immutable attachment policy.
func (*Adapter) SanitizeRequestWithPolicy(ctx context.Context, body []byte, s *transform.Sanitizer, mode string) ([]byte, error) {
	if mode != "extract_text" {
		return sanitizeRequest(body, s)
	}
	e := attachment.New(ctx)
	defer e.Close()
	return sanitizeWithExtractor(body, s, e)
}

// userMessage refuses ambiguous roles/fields before permitting conversion.
func userMessage(n *node) bool {
	seen := map[string]bool{}
	role := ""
	for i, k := range n.keys {
		if seen[k] {
			return false
		}
		seen[k] = true
		if k == "role" {
			if n.kids[i].kind != '"' {
				return false
			}
			role = n.kids[i].str
		}
		if k == "type" && (n.kids[i].kind != '"' || n.kids[i].str != "message") {
			return false
		}
	}
	return role == "user"
}

func (w reqWalker) attachment(n *node, sc scope, original error) error {
	var ae *attachment.Error
	if w.extract == nil || !errors.As(original, &ae) {
		return original
	}
	c := ae.Category
	fail := func(why attachment.Cause) error {
		return fmt.Errorf("%w: %w", adapter.ErrBadBody, &attachment.Error{Category: c, Cause: why})
	}
	if sc.pos != posPart || !sc.userContent {
		return fail(attachment.Shape)
	}
	typ := itemType(n)
	if typ != "input_file" && typ != "input_image" {
		return fail(attachment.Unsupported)
	}
	dataKey := "file_data"
	if typ == "input_image" {
		dataKey, c = "image_url", attachment.Image
	} else {
		c = attachment.File
	}
	seen := map[string]bool{}
	data := ""
	for i, k := range n.keys {
		if seen[k] {
			return fail(attachment.Shape)
		}
		seen[k] = true
		if k == "file_id" || k == "file_url" {
			return fail(attachment.Reference)
		}
		if k != "type" && k != dataKey && !(typ == "input_file" && k == "filename") && !(typ == "input_image" && k == "detail") {
			return fail(attachment.Shape)
		}
		v := n.kids[i]
		if v.kind != '"' {
			return fail(attachment.Shape)
		}
		if k == dataKey {
			data = v.str
		}
		if k == "detail" && v.str != "auto" && v.str != "low" && v.str != "high" {
			return fail(attachment.Shape)
		}
	}
	if data == "" {
		return fail(attachment.Shape)
	}
	text, err := w.extract.Extract(c, data)
	if err != nil {
		return fmt.Errorf("%w: %w", adapter.ErrBadBody, err)
	}
	// Replace the whole object, dropping filenames, metadata and binary bytes.
	t, v := &node{}, &node{}
	t.setString("input_text")
	v.setString(text)
	*n = node{kind: '{', keys: []string{"type", "text"}, kids: []*node{t, v}, dirty: true}
	return nil
}
