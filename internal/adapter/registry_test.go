package adapter

import (
	"testing"

	"cui-proxy/internal/transform"
)

type fake struct{ name string }

func (f fake) Name() string                     { return f.name }
func (fake) Match(string, string) (Route, bool) { return Route{}, false }
func (fake) SanitizeRequest(b []byte, _ *transform.Sanitizer) ([]byte, error) {
	return b, nil
}
func (fake) NewResponseRewriter(*transform.Restorer) ResponseRewriter { return nil }

func TestRegisterAndGet(t *testing.T) {
	Register(fake{"test-fake"})
	a, ok := Get("test-fake")
	if !ok || a.Name() != "test-fake" {
		t.Fatalf("Get = %v, %v", a, ok)
	}
	if _, ok := Get("missing"); ok {
		t.Error("Get(missing) found an adapter")
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate Register did not panic")
		}
	}()
	Register(fake{"test-fake"})
}
