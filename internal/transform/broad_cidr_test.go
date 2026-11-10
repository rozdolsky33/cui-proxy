package transform_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

func TestBroadCIDRRoundTripAndRepeatedSanitization(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"0.0.0.0/0", "128.0.0.0/1", "192.0.0.0/2", "192.0.0.0/3", "192.0.0.0/4", "192.0.0.0/5", "196.0.0.0/6", "198.0.0.0/7", "198.0.0.0/8"} {
		t.Run(input, func(t *testing.T) {
			v := vault.New(bytes.Repeat([]byte{7}, 32))
			s := transform.NewSanitizer(ds, v, nil)
			masked, err := s.Text(input)
			if err != nil {
				t.Fatalf("sanitization failed: %v", err)
			}
			if masked == input || masked == "" {
				t.Fatal("prefix was not masked")
			}
			if restored := transform.NewRestorer(v).Text(masked); restored != input {
				t.Fatal("prefix did not restore exactly")
			}
			again, err := s.Text(masked)
			if err != nil || again != masked {
				t.Fatalf("sanitization was not idempotent: %v", err)
			}
		})
	}
}

func TestBroadCIDRStillDisablesOverflowAllocation(t *testing.T) {
	v := vault.New(bytes.Repeat([]byte{7}, 32))
	v.Reserve("0.0.0.0/0")
	for i := 0; i < 762; i++ {
		masked, err := v.Pseudonym("ipv4", fmt.Sprintf("10.0.%d.%d", i/254, i%254+1))
		if err != nil || synth.InOverflow(masked) {
			t.Fatalf("documentation pool allocation failed: %v", err)
		}
	}
	if _, err := v.Pseudonym("ipv4", "10.1.0.1"); !errors.Is(err, synth.ErrExhausted) {
		t.Fatal("overflow allocation must remain disabled after a broad real prefix")
	}
}
