package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestObservabilityDefaultAndExplicit(t *testing.T) {
	var cfg Config
	if cfg.Observability.Correlate {
		t.Fatal("correlation defaults on")
	}
	if err := yaml.Unmarshal([]byte("observability:\n  correlate: true\n"), &cfg); err != nil || !cfg.Observability.Correlate {
		t.Fatal("explicit correlation not parsed")
	}
}
