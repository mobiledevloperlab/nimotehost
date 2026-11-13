// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// packageDistribution archives only this build's distribution, preserving Unix
// executable modes. Checksums are adjacent to each versioned download.
func packageDistribution(directory, targetOS, arch, version string) (string, error) {
	return packageNamedDistribution(directory, targetOS, arch, version, "nimote-host")
}
func packageNamedDistribution(directory, targetOS, arch, version, prefix string) (string, error) {
	extension := ".tar.gz"
	if targetOS == "windows" {
		extension = ".zip"
	}
	path := filepath.Join("dist", prefix+"-"+version+"-"+targetOS+"-"+arch+extension)
	files := []string{}
	if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink cannot enter distribution: %s", path)
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(files)
	output, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if targetOS == "windows" {
		writer := zip.NewWriter(output)
		for _, file := range files {
			if err := addZipFile(writer, directory, file); err != nil {
				writer.Close()
				output.Close()
				return "", err
			}
		}
		err = writer.Close()
	} else {
		compressed := gzip.NewWriter(output)
		writer := tar.NewWriter(compressed)
		for _, file := range files {
			if err := addTarFile(writer, directory, file); err != nil {
				writer.Close()
				compressed.Close()
				output.Close()
				return "", err
			}
		}
		err = writer.Close()
		if closeErr := compressed.Close(); err == nil {
			err = closeErr
		}
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, input)
	input.Close()
	if err != nil {
		return "", err
	}
	checksum := fmt.Sprintf("%x  %s\n", hash.Sum(nil), filepath.Base(path))
	if err := os.WriteFile(path+".sha256", []byte(checksum), 0644); err != nil {
		return "", err
	}
	return path, nil
}
func addZipFile(writer *zip.Writer, root, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(relative)
	header.SetMode(distributionMode(header.Name))
	header.Method = zip.Deflate
	header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	dest, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(dest, source)
	return err
}
func addTarFile(writer *tar.Writer, root, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(relative)
	header.Mode = int64(distributionMode(header.Name))
	header.ModTime = time.Unix(0, 0)
	header.Uid = 0
	header.Gid = 0
	header.Uname = ""
	header.Gname = ""
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(writer, source)
	return err
}

// Normalize portable distribution modes explicitly: Windows filesystem metadata
// cannot represent Unix executable bits when cross-packaging Linux/macOS builds.
func distributionMode(name string) os.FileMode {
	switch name {
	case "nimote-host", "nimote-host.exe", "nimote-host-gui", "nimote-host-gui.exe", "Nimote Host.app/Contents/MacOS/nimote-host", "Nimote Host.app/Contents/MacOS/nimote-host-gui", "scripts/install.sh":
		return 0755
	default:
		return 0644
	}
}
