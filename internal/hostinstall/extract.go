// SPDX-License-Identifier: Apache-2.0
package hostinstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Extract reads the release builder's regular-file archives into a new private
// directory. The caller verifies the archive digest before calling Extract.
func Extract(ctx context.Context, archive, dest string, windows bool) error {
	if err := os.Mkdir(dest, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	remaining := int64(512 << 20)
	count := 0
	write := func(name string, mode fs.FileMode, size int64, body io.Reader) error {
		count++
		if count > 10000 || size < 0 || size > remaining || !mode.IsRegular() ||
			name == "." || !fs.ValidPath(name) || !filepath.IsLocal(filepath.FromSlash(name)) ||
			strings.ContainsAny(name, "\\:\x00\r\n") {
			return ErrIntegrity
		}
		remaining -= size
		path := filepath.FromSlash(name)
		if err := root.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600|mode&0100)
		if err != nil {
			return err
		}
		n, err := io.Copy(file, io.LimitReader(&contextReader{ctx: ctx, r: body}, size+1))
		if n != size {
			err = errors.Join(err, ErrIntegrity)
		}
		return errors.Join(err, file.Close())
	}
	if windows {
		reader, err := zip.OpenReader(archive)
		if reader != nil {
			defer reader.Close()
		}
		if err != nil {
			return errors.Join(ErrIntegrity, err)
		}
		for _, entry := range reader.File {
			body, err := entry.Open()
			if err != nil {
				return errors.Join(ErrIntegrity, err)
			}
			err = write(entry.Name, entry.Mode(), int64(entry.UncompressedSize64), body)
			if err = errors.Join(err, body.Close()); err != nil {
				return err
			}
		}
		return nil
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	uncompressed, err := gzip.NewReader(file)
	if err != nil {
		return errors.Join(ErrIntegrity, err)
	}
	defer uncompressed.Close()
	reader := tar.NewReader(uncompressed)
	for {
		entry, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.Join(ErrIntegrity, err)
		}
		if entry.Typeflag != tar.TypeReg {
			return ErrIntegrity
		}
		if err := write(entry.Name, entry.FileInfo().Mode(), entry.Size, reader); err != nil {
			return err
		}
	}
}
