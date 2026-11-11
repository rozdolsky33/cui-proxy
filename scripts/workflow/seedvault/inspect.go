package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"cui-proxy/internal/vault"
)

// readPrivateObservationInput rejects symlinks, devices and public input files.
func readPrivateObservationInput(path string, maximum int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maximum {
		return nil, errors.New("invalid observation input")
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errors.New("oversized observation input")
	}
	return data, nil
}

// inspect observes an existing isolated vault without admitting or creating pairs.
// A private snapshot avoids reopening a path that the running proxy may replace.
func inspect(dir string) (int, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return 0, errors.New("observation directory must be private")
	}
	key, err := readPrivateObservationInput(filepath.Join(dir, "vault.key"), 32)
	if err != nil {
		return 0, err
	}
	if len(key) != 32 {
		return 0, errors.New("invalid observation key length")
	}
	blob, err := readPrivateObservationInput(filepath.Join(dir, "vault.enc"), 64<<20)
	if err != nil {
		return 0, err
	}
	snapshot, err := os.CreateTemp(dir, ".inspection-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(snapshot.Name())
	if _, err := snapshot.Write(blob); err != nil {
		snapshot.Close()
		return 0, err
	}
	if err := snapshot.Close(); err != nil {
		return 0, err
	}
	v, err := vault.Open(snapshot.Name(), key)
	if err != nil {
		return 0, err
	}
	mappings := make(map[string]string, v.Len())
	for _, synthetic := range v.Synthetics() {
		original, ok := v.Real(synthetic)
		if !ok || original == "" || original == synthetic {
			return 0, errors.New("invalid observed mapping")
		}
		if _, exists := mappings[original]; exists {
			return 0, errors.New("ambiguous observed mapping")
		}
		mappings[original] = synthetic
	}
	if len(mappings) != v.Len() {
		return 0, errors.New("incomplete observed population")
	}
	payload := struct {
		Population int               `json:"population"`
		Mappings   map[string]string `json:"mappings"`
	}{v.Len(), mappings}
	output, err := os.OpenFile(filepath.Join(dir, "observed.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	defer output.Close()
	if err := json.NewEncoder(output).Encode(payload); err != nil {
		return 0, err
	}
	if err := output.Sync(); err != nil {
		return 0, err
	}
	if err := output.Close(); err != nil {
		return 0, err
	}
	return v.Len(), nil
}
