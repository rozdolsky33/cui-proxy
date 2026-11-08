package manual

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteOfflineManual(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "Manual with spaces é")
	if err := WriteTo(dest); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "attachments.html", "menubar.html", "examples/attachment-text.yaml", "search/search_index.js"} {
		b, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || len(b) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dest, "index.html"))
	if !strings.Contains(string(b), "attachments.html") {
		t.Fatal("local navigation missing")
	}
	if strings.Contains(string(b), `src="https://`) {
		t.Fatal("automatic remote script")
	}
}

func TestSnapshotMatchesSources(t *testing.T) {
	dest := t.TempDir()
	if err := WriteTo(dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hashes map[string]string
	if err := json.Unmarshal(b, &hashes); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..")
	var expected []string
	for _, dir := range []string{"docs", "scripts/manual-assets"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == "docs/superpowers" || rel == "docs/adr" || rel == "docs/spikes" || rel == "docs/verification-results" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") { // .DS_Store and editor files are not sources
				return nil
			}
			expected = append(expected, rel)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	expected = append(expected, "mkdocs.yml", "mkdocs-offline.yml", "requirements-docs.txt", "scripts/build-manual.py")
	if len(hashes) != len(expected) {
		t.Fatal("manual source list changed; regenerate with scripts/build-manual.py")
	}
	for _, path := range expected {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if hashes[path] != fmt.Sprintf("%x", sha256.Sum256(b)) {
			t.Errorf("stale manual: %s; regenerate with scripts/build-manual.py", path)
		}
	}
}

func TestWriteManualRefusesSymlinkEscape(t *testing.T) {
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "assets")); err != nil {
		t.Skip(err)
	}
	if err := WriteTo(dest); err == nil {
		t.Fatal("followed symlink outside manual root")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v", err)
	}
}

func TestOfflineLinksResolveToFiles(t *testing.T) {
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	links := regexp.MustCompile(`href="([^"]+)"`)
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, ".html") || f.Name == "404.html" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range links.FindAllSubmatch(b, -1) {
			u, err := url.Parse(html.UnescapeString(string(match[1])))
			if err != nil {
				t.Fatal(err)
			}
			if u.IsAbs() || u.Host != "" || u.Path == "" {
				continue
			}
			target := path.Clean(path.Join(path.Dir(f.Name), u.Path))
			got, err := z.Open(target)
			if err != nil {
				t.Errorf("%s link %s is not a bundled file: %v", f.Name, u.Path, err)
				continue
			}
			stat, err := got.Stat()
			got.Close()
			if err != nil || stat.IsDir() {
				t.Errorf("%s link %s opens a directory", f.Name, u.Path)
			}
		}
	}
}
