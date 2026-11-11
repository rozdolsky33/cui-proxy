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
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
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
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(dir); err == nil {
		t.Fatal("missing vault accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("inspection created private state")
	}
}

func TestInspectRejectsUnsafeOrCorruptInputs(t *testing.T) {
	for _, kind := range []string{"public-key", "symlink-key", "corrupt-vault", "missing-key"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			key := bytes.Repeat([]byte{9}, 32)
			keyPath := filepath.Join(dir, "vault.key")
			vaultPath := filepath.Join(dir, "vault.enc")
			if err := os.WriteFile(keyPath, key, 0600); err != nil {
				t.Fatal(err)
			}
			v := vault.New(key)
			if _, err := v.Pseudonym("email", "observation@example.invalid"); err != nil {
				t.Fatal(err)
			}
			if err := v.Save(vaultPath); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "public-key":
				if err := os.Chmod(keyPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink-key":
				if err := os.Rename(keyPath, keyPath+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(keyPath+".saved", keyPath); err != nil {
					t.Fatal(err)
				}
			case "corrupt-vault":
				if err := os.WriteFile(vaultPath, []byte("invalid encrypted observation"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-key":
				if err := os.Remove(keyPath); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(vaultPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := inspect(dir); err == nil {
				t.Fatal("unsafe observation accepted")
			}
			after, err := os.ReadFile(vaultPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("inspection modified source vault")
			}
			if _, err := os.Lstat(filepath.Join(dir, "observed.json")); !os.IsNotExist(err) {
				t.Fatal("failed observation published evidence")
			}
		})
	}
}

func TestInspectRejectsPublicDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(dir); err == nil {
		t.Fatal("public directory accepted")
	}
}
