package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cui-proxy/internal/vault"
)

func TestInspectExistingVaultDoesNotCreateMappings(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{7}, 32)
	keyPath := filepath.Join(dir, "vault.key")
	vaultPath := filepath.Join(dir, "vault.enc")
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	v := vault.New(key)
	synthetic, err := v.Pseudonym("email", "churn@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Save(vaultPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	count, err := inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("unexpected population")
	}
	after, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("inspection mutated vault")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "observed.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Population int               `json:"population"`
		Mappings   map[string]string `json:"mappings"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Population != 1 || len(result.Mappings) != 1 || result.Mappings["churn@example.invalid"] != synthetic {
		t.Fatal("incorrect observed mapping")
	}
	info, err := os.Stat(filepath.Join(dir, "observed.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe observation permissions")
	}
	if _, err := inspect(dir); err == nil {
		t.Fatal("existing evidence overwritten")
	}
}

func TestInspectMissingVaultCannotInventEvidence(t *testing.T) {
	dir := t.TempDir()
	if _, err := inspect(dir); err == nil {
		t.Fatal("missing vault accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("inspection created private state")
	}
}
