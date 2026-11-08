// Package manual holds the versioned, offline operator documentation shipped
// inside every binary, including standalone shell and Homebrew installations.
package manual

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

//go:embed site.zip
var archive []byte

// WriteTo installs the snapshot into a private staging directory. Callers must
// finish extraction before signing or replacing an existing application.
func WriteTo(dir string) error {
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("manual: read bundled archive: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range z.File {
		if !fs.ValidPath(f.Name) || strings.Contains(f.Name, "\\") || !f.Mode().IsRegular() {
			return fmt.Errorf("manual: invalid archive entry")
		}
		if err := root.MkdirAll(path.Dir(f.Name), 0755); err != nil {
			return err
		}
		if err := writeFile(root, f); err != nil {
			return fmt.Errorf("manual: extract bundle: %w", err)
		}
	}
	return nil
}

func writeFile(root *os.Root, f *zip.File) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dest, err := root.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dest, src)
	closeErr := dest.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
