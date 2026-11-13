// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Package hostinstall stages complete, verified distributions for per-user setup.
// It never downloads code, extracts archives, changes SSH, or handles credentials.
package hostinstall

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Verify checks every file against SHA256SUMS and rejects unlisted files,
// symlinks, special files and unsafe paths. Checksums do not authenticate a publisher.
func Verify(ctx context.Context, directory string) error {
	return copyVerified(ctx, directory, "")
}

func copyVerified(ctx context.Context, source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("bundle must be a real directory")
	}
	manifest := filepath.Join(source, "SHA256SUMS")
	info, err = os.Lstat(manifest)
	if err != nil {
		return fmt.Errorf("complete distribution required (SHA256SUMS): %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return errors.New("invalid checksum manifest")
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	sums := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 67 || line[64:66] != "  " {
			return errors.New("invalid checksum entry")
		}
		name := line[66:]
		if name == "." || name == "SHA256SUMS" || !fs.ValidPath(name) || !filepath.IsLocal(filepath.FromSlash(name)) || strings.ContainsAny(name, "\\:\x00\r\n") {
			return fmt.Errorf("unsafe bundle path %q", name)
		}
		digest, err := hex.DecodeString(line[:64])
		if err != nil || len(digest) != sha256.Size {
			return errors.New("invalid SHA256 digest")
		}
		if _, ok := sums[name]; ok {
			return fmt.Errorf("duplicate bundle path %q", name)
		}
		sums[name] = strings.ToLower(line[:64])
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, name := range []string{"LICENSE", "NOTICE", "BUILD.json", "MODULES.json"} {
		if _, ok := sums[name]; !ok {
			return fmt.Errorf("incomplete distribution: missing %s", name)
		}
	}
	if _, ok := sums["nimote-host"]; !ok {
		if _, ok := sums["nimote-host.exe"]; !ok {
			return errors.New("incomplete distribution: missing Host binary")
		}
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink excluded from bundle: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special file excluded: %s", name)
		}
		if name == "SHA256SUMS" {
			return nil
		}
		expected, ok := sums[name]
		if !ok {
			return fmt.Errorf("unlisted bundle file: %s", name)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		hash := sha256.New()
		var writer io.Writer = hash
		var output *os.File
		if target != "" {
			dest := filepath.Join(target, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if info.Mode()&0111 != 0 {
				mode = 0755
			}
			output, err = os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			defer output.Close()
			writer = io.MultiWriter(hash, output)
		}
		if _, err := io.Copy(writer, &contextReader{ctx: ctx, r: input}); err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != expected {
			return fmt.Errorf("checksum mismatch: %s", name)
		}
		if output != nil {
			if err := output.Sync(); err != nil {
				return err
			}
			if err := output.Close(); err != nil {
				return err
			}
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(sums) {
		return errors.New("bundle is missing files listed in SHA256SUMS")
	}
	if target != "" {
		return os.WriteFile(filepath.Join(target, "SHA256SUMS"), data, 0644)
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
