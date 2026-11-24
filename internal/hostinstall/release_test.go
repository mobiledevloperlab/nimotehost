// SPDX-License-Identifier: Apache-2.0
package hostinstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCommunityReleaseSelection(t *testing.T) {
	for _, platform := range []struct{ os, arch, extension string }{
		{"linux", "amd64", ".tar.gz"}, {"linux", "arm64", ".tar.gz"},
		{"darwin", "amd64", ".tar.gz"}, {"darwin", "arm64", ".tar.gz"},
		{"windows", "amd64", ".zip"}, {"windows", "arm64", ".zip"},
		{"linux", "386", ".tar.gz"}, {"windows", "386", ".zip"},
	} {
		t.Run(platform.os+"/"+platform.arch, func(t *testing.T) {
			for _, desktop := range []bool{false, true} {
				if desktop && platform.arch == "386" {
					continue
				}
				prefix := "nimotehost-"
				if desktop {
					prefix += "gui-"
				}
				name := prefix + "2.5.0-community-" + platform.os + "-" + platform.arch + platform.extension
				asset := ReleaseAsset{Name: name, URL: "https://github.com/" + repository + "/releases/download/community-v2.5.0/" + name, State: "uploaded", Size: 10, Digest: fmt.Sprintf("sha256:%064x", 1)}
				r := releaseMetadata{Tag: "community-v2.5.0", Assets: []ReleaseAsset{asset}}
				got, err := r.selectAsset("community-v2.5.0", platform.os, platform.arch, desktop)
				if err != nil || got != asset {
					t.Fatalf("%+v, %v", got, err)
				}
				if _, err := r.selectAsset("", platform.os, "wrong", desktop); !errors.Is(err, ErrRelease) {
					t.Fatalf("wrong asset selected: %v", err)
				}
				r.Assets[0].Digest = ""
				if _, err := r.selectAsset("", platform.os, platform.arch, desktop); !errors.Is(err, ErrIntegrity) {
					t.Fatalf("missing digest: %v", err)
				}
				r.Draft = true
				if _, err := r.selectAsset("", platform.os, platform.arch, desktop); !errors.Is(err, ErrRelease) {
					t.Fatalf("draft: %v", err)
				}
				r.Draft, r.Prerelease = false, true
				if _, err := r.selectAsset("", platform.os, platform.arch, desktop); !errors.Is(err, ErrRelease) {
					t.Fatalf("prerelease: %v", err)
				}
			}
		})
	}
	if _, _, err := ResolveRelease(context.Background(), "", "linux", "riscv64", false); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unexpected unsupported architecture result: %v", err)
	}
}

func TestDownloadRetriesAndVerification(t *testing.T) {
	const payload = "authentic release"
	for _, test := range []struct {
		name      string
		responses []string
		want      error
	}{
		{"transient then truncated then success", []string{"503", "short", payload}, nil},
		{"always truncated", []string{"short", "short", "short"}, ErrRelease},
		{"wrong SHA256", []string{"modified release!"}, ErrIntegrity},
	} {
		t.Run(test.name, func(t *testing.T) {
			var count atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(count.Add(1)) - 1
				if i >= len(test.responses) || test.responses[i] == "503" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				fmt.Fprint(w, test.responses[i])
			}))
			defer server.Close()
			asset := ReleaseAsset{Name: "test", URL: server.URL, Size: int64(len(payload)), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(payload)))}
			dest := filepath.Join(t.TempDir(), "archive")
			if err := asset.download(context.Background(), server.Client(), dest); !errors.Is(err, test.want) {
				t.Fatalf("%v, want %v", err, test.want)
			}
			if got := int(count.Load()); got != len(test.responses) {
				t.Fatalf("download got %d attempts", got)
			}
			data, err := os.ReadFile(dest)
			if test.want == nil && (err != nil || string(data) != payload) || test.want != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected staged archive %q: %v", data, err)
			}
		})
	}
}

func TestExtractPortableArchives(t *testing.T) {
	for _, windows := range []bool{false, true} {
		for _, name := range []string{"file", "../escape", "nested\\escape"} {
			t.Run(fmt.Sprintf("windows=%v/%s", windows, name), func(t *testing.T) {
				dir := t.TempDir()
				archive := filepath.Join(dir, "archive")
				file, err := os.Create(archive)
				if err != nil {
					t.Fatal(err)
				}
				if windows {
					zipper := zip.NewWriter(file)
					header := &zip.FileHeader{Name: name}
					header.SetMode(0755)
					entry, err := zipper.CreateHeader(header)
					if err != nil {
						t.Fatal(err)
					}
					_, err = fmt.Fprint(entry, "data")
					if err = errors.Join(err, zipper.Close()); err != nil {
						t.Fatal(err)
					}
				} else {
					compressed := gzip.NewWriter(file)
					tarball := tar.NewWriter(compressed)
					if err := tarball.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: 4}); err != nil {
						t.Fatal(err)
					}
					_, err := fmt.Fprint(tarball, "data")
					if err = errors.Join(err, tarball.Close(), compressed.Close()); err != nil {
						t.Fatal(err)
					}
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				dest := filepath.Join(dir, "bundle")
				err = Extract(context.Background(), archive, dest, windows)
				if name == "file" {
					data, readErr := os.ReadFile(filepath.Join(dest, name))
					if err != nil || readErr != nil || string(data) != "data" {
						t.Fatalf("extract=%v read=%v content=%q", err, readErr, data)
					}
				} else if !errors.Is(err, ErrIntegrity) {
					t.Fatalf("unexpected unsafe entry result: %v", err)
				}
			})
		}
	}
}

func TestCommunityLookupSelectsPublishedPackageForTarget(t *testing.T) {
	const cliTag = "community-v2.5.1"
	const desktopTag = "community-v2.5.0"
	asset := func(tag, prefix string) ReleaseAsset {
		name := prefix + CommunityVersion(tag) + "-darwin-arm64.tar.gz"
		return ReleaseAsset{Name: name, URL: "https://github.com/" + repository + "/releases/download/" + tag + "/" + name, State: "uploaded", Size: 10, Digest: fmt.Sprintf("sha256:%064x", 1)}
	}
	rows := []releaseMetadata{
		{Tag: "v2.4.1"},
		{Tag: "v2.5.0-rc.1", Prerelease: true},
		{Tag: "community-v2.5.3", Draft: true},
		{Tag: "community-v2.5.2", Prerelease: true, Assets: []ReleaseAsset{asset("community-v2.5.2", "nimotehost-")}},
		{Tag: cliTag, Assets: []ReleaseAsset{asset(cliTag, "nimotehost-")}},
		{Tag: desktopTag, Assets: []ReleaseAsset{asset(desktopTag, "nimotehost-gui-")}},
	}
	client := &http.Client{Transport: releaseTestTransport(func(r *http.Request) (*http.Response, error) {
		var body []byte
		switch r.URL.String() {
		case releaseAPI + "?per_page=30":
			body, _ = json.Marshal(rows)
		case releaseAPI + "/tags/" + cliTag:
			body, _ = json.Marshal(rows[4])
		default:
			t.Fatalf("wrong release endpoint: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	ctx := context.Background()
	for _, test := range []struct {
		desktop bool
		want    string
	}{{false, cliTag}, {true, desktopTag}} {
		if got, _, err := communityRelease(ctx, client, "", "darwin", "arm64", test.desktop); err != nil || got != test.want {
			t.Fatalf("desktop=%v: %s, %v", test.desktop, got, err)
		}
	}
	if _, _, err := communityRelease(ctx, client, cliTag, "darwin", "arm64", true); !errors.Is(err, ErrRelease) {
		t.Fatalf("pinned tag without desktop asset: %v", err)
	}
	if _, _, err := communityRelease(ctx, client, "v2.4.1", "darwin", "arm64", false); !errors.Is(err, ErrRelease) {
		t.Fatalf("legacy tag: %v", err)
	}
}

type releaseTestTransport func(*http.Request) (*http.Response, error)

func (fn releaseTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
