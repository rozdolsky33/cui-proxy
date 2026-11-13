package recovery

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGuidanceCategories(t *testing.T) {
	for _, c := range []struct{ reason, kind, detail, category, retry string }{
		{"policy", "rule_match", "", "policy", "after_change"},
		{"sanitize", "bad_body", "attachment_image_unsupported", "representation", "after_change"},
		{"sanitize", "bad_body", "attachment_image_missing_tool", "dependency", "after_repair"},
		{"sanitize", "bad_body", "attachment_image_limit", "budget", "after_change"},
		{"sanitize", "bad_body", "attachment_image_busy", "overload", "backoff"},
		{"sanitize", "pool_exhausted", "", "budget", "after_change"},
		{"upstream", "", "", "transient", "backoff"},
	} {
		g := Lookup(c.reason, c.kind, c.detail)
		if g.Category != c.category || g.Retry != c.retry || g.Remedy == "" || g.Code == "" {
			t.Errorf("wrong guidance category: %s", c.category)
		}
	}
}
func TestUnknownGuidanceIsValueFree(t *testing.T) {
	g := Lookup("arbitrary-private-value", "arbitrary-private-value", "arbitrary-private-value")
	b, _ := json.Marshal(g)
	if strings.Contains(string(b), "arbitrary-private-value") {
		t.Fatal("input echoed")
	}
	if g.Retry != "after_repair" {
		t.Fatal("unknown failure invites retry")
	}
}
func TestRecurrenceBoundedAndExpires(t *testing.T) {
	var tracker Tracker
	now := time.Now()
	if tracker.Record("attachment_image_unsupported", now) != 1 || tracker.Record("attachment_image_unsupported", now) != 2 {
		t.Fatal("recurrence not counted")
	}
	if tracker.Record("attachment_image_unsupported", now.Add(time.Minute)) != 1 {
		t.Fatal("window did not expire")
	}
	if tracker.Record("unknown-value", now) != 0 || tracker.Record("attachment_image_busy", now) != 0 {
		t.Fatal("untrusted or transient code counted")
	}
	var group sync.WaitGroup
	for i := 0; i < 64; i++ {
		group.Add(1)
		go func() { defer group.Done(); tracker.Record("policy", now.Add(time.Minute)) }()
	}
	group.Wait()
	if tracker.Record("policy", now.Add(time.Minute)) != 65 {
		t.Fatal("concurrent count lost")
	}
}

func TestMalformedMetadataIsRepresentationNotPolicy(t *testing.T) {
	g := Lookup("header", "bad_body", "")
	if g.Category != "representation" || g.Retry != "after_change" {
		t.Fatal("malformed metadata misclassified as policy")
	}
}
