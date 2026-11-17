// SPDX-License-Identifier: Apache-2.0
package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func catalogFixture(t *testing.T) (*Catalog, *Runtime, CatalogEntry, []byte, []byte) {
	t.Helper()
	source, _ := bundle(t, "community-echo")
	manifest, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(filepath.Join(source, "plugin"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(manifest)
	entry := CatalogEntry{ID: "community-echo", Name: "Community Echo", Description: "A separately developed plugin", Repository: "author/echo", Version: "1.0.0", Tag: "v1.0.0", Assets: map[string]CatalogAsset{runtime.GOOS + "/" + runtime.GOARCH: {Manifest: "echo.manifest.json", SHA256: hex.EncodeToString(hash[:]), Binary: "echo.binary"}}}
	c, err := NewCatalog(filepath.Join(t.TempDir(), "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	var count atomic.Int32
	r, err := New(filepath.Join(t.TempDir(), "plugins"), "epoch", fakeLauncher(&count))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	setCatalogTransport(t, c, entry, manifest, binary)
	return c, r, entry, manifest, binary
}
func setCatalogTransport(t *testing.T, c *Catalog, entry CatalogEntry, manifest, binary []byte) {
	t.Helper()
	index, _ := json.Marshal(CatalogIndex{Version: "1.0", Plugins: []CatalogEntry{entry}})
	c.client = &http.Client{CheckRedirect: newCatalogClient().CheckRedirect, Transport: catalogTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
			t.Error("credentials forwarded")
		}
		var raw []byte
		switch req.URL.Path {
		case "/mobiledevloperlab/nimotehost/master/plugin-catalog/index.json":
			raw = index
		case "/author/echo/releases/download/v1.0.0/echo.manifest.json":
			raw = manifest
		case "/author/echo/releases/download/v1.0.0/echo.binary":
			raw = binary
		default:
			return nil, errors.New("unexpected URL")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: http.Header{}, ContentLength: int64(len(raw)), Request: req}, nil
	})}
}
func waitInstall(t *testing.T, c *Catalog, id string) InstallJob {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		job, err := c.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State != "downloading" && job.State != "installing" {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("install did not finish")
	return InstallJob{}
}
func TestCatalogSearchReviewInstallAndExplicitUpdate(t *testing.T) {
	c, r, entry, manifest, binary := catalogFixture(t)
	result := c.Search(context.Background(), "echo")
	if len(result.Failures) != 0 || len(result.Plugins) != 1 || !result.Plugins[0].Compatible {
		t.Fatal(result)
	}
	if len(c.Search(context.Background(), "not-found").Plugins) != 0 {
		t.Fatal("search filter ignored")
	}
	offer, err := c.Offer(context.Background(), "community", entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if offer.Manifest.ID != entry.ID || offer.SHA256 != entry.Assets[runtime.GOOS+"/"+runtime.GOARCH].SHA256 {
		t.Fatal(offer)
	}
	if _, err = c.StartInstall(r, "community", entry.ID, offer.SHA256, false, false); err == nil {
		t.Fatal("installed without confirmation")
	}
	job, err := c.StartInstall(r, "community", entry.ID, offer.SHA256, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitInstall(t, c, job.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
	if len(r.List()) != 1 || r.List()[0].State != "disabled" || len(r.List()[0].Grants) != 0 {
		t.Fatal("remote install executed or granted permissions", r.List())
	}
	if _, err = c.StartInstall(r, "community", entry.ID, offer.SHA256, false, true); err == nil {
		t.Fatal("install overwrote existing plugin")
	}
	if err = r.Enable(entry.ID, []string{"process.same-user"}, true); err != nil {
		t.Fatal(err)
	}
	waitReady(t, r, entry.ID)
	if _, err = c.StartInstall(r, "community", entry.ID, offer.SHA256, true, true); err == nil {
		t.Fatal("enabled update allowed")
	}
	if err = r.Disable(entry.ID); err != nil {
		t.Fatal(err)
	}
	setCatalogTransport(t, c, entry, manifest, binary)
	job, err = c.StartInstall(r, "community", entry.ID, offer.SHA256, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if waitInstall(t, c, job.ID).State != "succeeded" {
		t.Fatal("disabled update failed")
	}
	if len(c.Jobs()) != 2 {
		t.Fatal("install jobs not recoverable after GUI reconnect")
	}
}
func TestCatalogIntegrityFailuresNeverPublish(t *testing.T) {
	for _, variant := range []string{"manifest", "binary", "changed-review", "identity", "platform"} {
		t.Run(variant, func(t *testing.T) {
			c, r, entry, manifest, binary := catalogFixture(t)
			asset := entry.Assets[runtime.GOOS+"/"+runtime.GOARCH]
			expected := asset.SHA256
			switch variant {
			case "manifest":
				manifest = []byte(`{}`)
			case "binary":
				binary = []byte("tampered")
			case "changed-review":
				expected = strings.Repeat("a", 64)
			case "identity":
				var m sdk.Manifest
				_ = json.Unmarshal(manifest, &m)
				m.ID = "different"
				manifest, _ = json.Marshal(m)
				sum := sha256.Sum256(manifest)
				asset.SHA256 = hex.EncodeToString(sum[:])
				entry.Assets[runtime.GOOS+"/"+runtime.GOARCH] = asset
				expected = asset.SHA256
			case "platform":
				entry.Assets = map[string]CatalogAsset{"linux/386": asset}
				if runtime.GOOS == "linux" && runtime.GOARCH == "386" {
					entry.Assets = map[string]CatalogAsset{"darwin/arm64": asset}
				}
			}
			setCatalogTransport(t, c, entry, manifest, binary)
			job, err := c.StartInstall(r, "community", entry.ID, expected, false, true)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitInstall(t, c, job.ID); got.State != "failed" {
				t.Fatal(got)
			}
			if len(r.List()) != 0 {
				t.Fatal("invalid executable published")
			}
		})
	}
}
func TestCatalogSourceConfirmationPersistenceAndURLPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	c, err := NewCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	location := "https://raw.githubusercontent.com/author/plugins/main/index.json"
	if err = c.AddSource("third-party", location, false); err == nil {
		t.Fatal("source accepted without confirmation")
	}
	if err = c.AddSource("third-party", location, true); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(reopened.Sources()) != 2 {
		t.Fatal("source not persisted")
	}
	if err = c.RemoveSource("third-party"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://raw.githubusercontent.com/a/b/main/index.json", "https://localhost/index.json", "https://raw.githubusercontent.com.evil/a/b/main/i.json", "https://user:SECRET@raw.githubusercontent.com/a/b/main/i.json", "https://raw.githubusercontent.com/a/b/main/../i.json", "https://raw.githubusercontent.com/a/b/main/%2e%2e/i.json", "https://raw.githubusercontent.com/a/b/main/i.json?token=SECRET", "https://raw.githubusercontent.com:443/a/b/main/i.json"} {
		if catalogURL(raw) {
			t.Error("unsafe source", raw)
		}
	}
	client := newCatalogClient()
	for _, raw := range []string{"http://github.com/a/b", "https://127.0.0.1/", "https://github.com.evil/", "https://user:SECRET@github.com/a"} {
		u, _ := url.Parse(raw)
		if err = client.CheckRedirect(&http.Request{URL: u}, []*http.Request{{}}); err == nil {
			t.Error("unsafe redirect", raw)
		}
	}
	u, _ := url.Parse("https://release-assets.githubusercontent.com/github-production-release-asset/asset?token=signed")
	if err = client.CheckRedirect(&http.Request{URL: u}, []*http.Request{{}}); err != nil {
		t.Fatal(err)
	}
}
func TestCatalogDownloadCancelAndSourceRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "revoke"}[revoke], func(t *testing.T) {
			c, r, entry, manifest, _ := catalogFixture(t)
			index, _ := json.Marshal(CatalogIndex{Version: "1.0", Plugins: []CatalogEntry{entry}})
			started := make(chan struct{})
			c.client = &http.Client{Transport: catalogTransport(func(req *http.Request) (*http.Response, error) {
				var raw []byte
				if strings.HasSuffix(req.URL.Path, "index.json") {
					raw = index
				} else if strings.HasSuffix(req.URL.Path, ".manifest.json") {
					raw = manifest
				} else {
					close(started)
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: http.Header{}, ContentLength: int64(len(raw))}, nil
			})}
			job, err := c.StartInstall(r, "community", entry.ID, entry.Assets[runtime.GOOS+"/"+runtime.GOARCH].SHA256, false, true)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("download not started")
			}
			if revoke {
				err = c.RemoveSource("community")
			} else {
				err = c.CancelInstall(job.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := waitInstall(t, c, job.ID); got.State != "canceled" {
				t.Fatal(got)
			}
			if len(r.List()) != 0 {
				t.Fatal("canceled download installed")
			}
			files, _ := os.ReadDir(r.dir)
			for _, f := range files {
				if strings.HasPrefix(f.Name(), ".download-") {
					t.Fatal("download staging leaked")
				}
			}
		})
	}
}
func TestCatalogRedirectAndDownloadBounds(t *testing.T) {
	c, _, _, _, _ := catalogFixture(t)
	c.client = &http.Client{CheckRedirect: newCatalogClient().CheckRedirect, Transport: catalogTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"http://169.254.169.254/SECRET"}}, Request: req}, nil
	})}
	var out bytes.Buffer
	if err := c.fetch(context.Background(), DefaultCatalogURL, 1024, &out, nil); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("redirect bypass or leak", err)
	}
	c.client = &http.Client{Transport: catalogTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 1025))), Header: http.Header{}, ContentLength: -1}, nil
	})}
	if err := c.fetch(context.Background(), DefaultCatalogURL, 1024, &out, nil); err == nil {
		t.Fatal("stream limit ignored")
	}
}

func TestPublicCatalogMetadataSchemaAndStarter(t *testing.T) {
	raw, err := os.ReadFile("../../docs/plugin-system/spec/v1/catalog.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../plugin-catalog/index.json")
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(value); err != nil {
		t.Fatal(err)
	}
	var index CatalogIndex
	if err = json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	if err = validateIndex(index); err != nil {
		t.Fatal(err)
	}
	_, _, entry, _, _ := catalogFixture(t)
	index.Plugins = []CatalogEntry{entry}
	raw, _ = json.Marshal(index)
	_ = json.Unmarshal(raw, &value)
	if err = schema.Validate(value); err != nil {
		t.Fatal("developer-only metadata invalid", err)
	}
	if normalized := normalizeCatalogURL("https://github.com/author/catalog/blob/main/index.json"); normalized != "https://raw.githubusercontent.com/author/catalog/main/index.json" {
		t.Fatal(normalized)
	}
}
