// seedvault prepares a private benchmark vault before the proxy starts.
// It is test tooling, not an interface for modifying a running vault.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/vault"
)

func seed(dir string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "pairs.json"))
	if err != nil {
		return 0, err
	}
	var pairs [][2]string
	if err = json.Unmarshal(raw, &pairs); err != nil {
		return 0, err
	}
	if len(pairs) == 0 {
		return 0, errors.New("empty population")
	}
	key, err := vault.LoadOrCreateKey(filepath.Join(dir, "vault.key"))
	if err != nil {
		return 0, err
	}
	v := vault.New(key)
	items := make([]vault.Item, len(pairs))
	for i, p := range pairs {
		items[i] = vault.Item{Class: p[0], Real: p[1]}
	}
	if _, err = v.Admit(items); err != nil {
		return 0, err
	}
	mappings := make(map[string]string, len(pairs))
	for _, p := range pairs {
		syn, e := v.Pseudonym(p[0], p[1])
		if e != nil {
			return 0, e
		}
		mappings[p[1]] = syn
	}
	if v.Len() != len(pairs) {
		return 0, errors.New("population mismatch")
	}
	if err = v.Save(filepath.Join(dir, "vault.enc")); err != nil {
		return 0, err
	}
	raw, err = json.Marshal(mappings)
	if err != nil {
		return 0, err
	}
	if err = os.WriteFile(filepath.Join(dir, "oracle.json"), raw, 0600); err != nil {
		return 0, err
	}
	return v.Len(), nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: seedvault PRIVATE_DIRECTORY")
		os.Exit(2)
	}
	n, err := seed(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture seed failed")
		os.Exit(1)
	}
	c, err := config.Load(filepath.Join(os.Args[1], "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture config invalid")
		os.Exit(1)
	}
	ds, err := detect.Build(c.Rules, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture detectors invalid")
		os.Exit(1)
	}
	metadata, err := json.Marshal(map[string]int{"vault_entries": n, "compiled_detectors": len(ds)})
	if err != nil || os.WriteFile(filepath.Join(os.Args[1], "population.json"), metadata, 0600) != nil {
		fmt.Fprintln(os.Stderr, "fixture metadata failed")
		os.Exit(1)
	}
	fmt.Printf("vault_entries=%d detectors=%d\n", n, len(ds))
}
