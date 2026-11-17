// SPDX-License-Identifier: Apache-2.0
package plugins

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk/publichttp"
)

const DefaultCatalogURL = "https://raw.githubusercontent.com/mobiledevloperlab/nimotehost/master/plugin-catalog/index.json"

var githubPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var assetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type CatalogSource = sdk.CatalogSource
type CatalogAsset = sdk.CatalogAsset
type CatalogEntry = sdk.CatalogEntry
type CatalogIndex = sdk.CatalogIndex
type CatalogMatch = sdk.CatalogMatch
type CatalogFailure = sdk.CatalogFailure
type CatalogSearch = sdk.CatalogSearch

type CatalogOffer struct {
	Entry       CatalogMatch `json:"entry"`
	Manifest    sdk.Manifest `json:"manifest"`
	SHA256      string       `json:"sha256"`
	manifestRaw []byte
}
type InstallJob struct {
	ID       string     `json:"id"`
	Plugin   string     `json:"plugin"`
	Source   string     `json:"source"`
	State    string     `json:"state"`
	Progress float64    `json:"progress"`
	Bytes    int64      `json:"bytes"`
	Error    *sdk.Error `json:"error,omitempty"`
	cancel   context.CancelFunc
}
type Catalog struct {
	mu        sync.Mutex
	statePath string
	sources   []CatalogSource
	client    *http.Client
	jobs      map[string]*InstallJob
	order     []string
	closed    bool
	wg        sync.WaitGroup
}

func normalizeCatalogURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "https" && u.Host == "github.com" && u.RawPath == "" {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) >= 5 && parts[2] == "blob" {
			u.Host = "raw.githubusercontent.com"
			u.Path = "/" + strings.Join(append(parts[:2], parts[3:]...), "/")
			return u.String()
		}
	}
	return raw
}
func catalogURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "raw.githubusercontent.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 4 || len(parts) > 16 || len(u.Path) > 1024 {
		return false
	}
	for _, part := range parts {
		if !assetName.MatchString(part) || part == "." || part == ".." {
			return false
		}
	}
	return githubPart.MatchString(parts[0]) && githubPart.MatchString(parts[1]) && strings.HasSuffix(u.Path, ".json")
}
func githubDownloadURL(u *url.URL, redirect bool) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" || u.Opaque != "" {
		return false
	}
	if !redirect {
		return u.Host == "raw.githubusercontent.com" || u.Host == "github.com"
	}
	switch u.Host {
	case "github.com", "raw.githubusercontent.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func newCatalogClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: publichttp.DialContext, DisableCompression: true, MaxResponseHeaderBytes: 32 << 10, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 || !githubDownloadURL(req.URL, true) {
			return errors.New("unsafe download redirect")
		}
		return nil
	}}
}
func NewCatalog(statePath string) (*Catalog, error) {
	c := &Catalog{statePath: statePath, client: newCatalogClient(), sources: []CatalogSource{{ID: "community", URL: DefaultCatalogURL}}, jobs: map[string]*InstallJob{}}
	raw, err := readRegular(statePath, 16<<10)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &c.sources); err != nil {
		return nil, errors.New("invalid catalog sources")
	}
	if len(c.sources) > 8 {
		return nil, errors.New("too many catalog sources")
	}
	seen := map[string]bool{}
	for _, s := range c.sources {
		if !sdk.Name.MatchString(s.ID) || !catalogURL(s.URL) || seen[s.ID] {
			return nil, errors.New("invalid catalog source")
		}
		seen[s.ID] = true
	}
	return c, nil
}
func (c *Catalog) Sources() []CatalogSource {
	c.mu.Lock()
	defer c.mu.Unlock()
	return clone(c.sources)
}
func (c *Catalog) saveSources(next []CatalogSource) error {
	if err := os.MkdirAll(filepath.Dir(c.statePath), 0700); err != nil {
		return err
	}
	raw, _ := json.Marshal(next)
	f, err := os.CreateTemp(filepath.Dir(c.statePath), ".catalog-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.statePath)
}
func (c *Catalog) AddSource(id, source string, confirmed bool) error {
	source = normalizeCatalogURL(source)
	if !confirmed || !sdk.Name.MatchString(id) || !catalogURL(source) {
		return errors.New("confirmed catalog ID and public GitHub raw JSON URL required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("catalog closed")
	}
	if len(c.sources) >= 8 {
		return errors.New("catalog source capacity reached")
	}
	for _, s := range c.sources {
		if s.ID == id || s.URL == source {
			return errors.New("catalog source already configured")
		}
	}
	next := append(append([]CatalogSource{}, c.sources...), CatalogSource{ID: id, URL: source})
	if err := c.saveSources(next); err != nil {
		return err
	}
	c.sources = next
	slog.Info("plugin catalog source added", "id", id)
	return nil
}
func (c *Catalog) RemoveSource(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := []CatalogSource{}
	found := false
	for _, s := range c.sources {
		if s.ID == id {
			found = true
		} else {
			next = append(next, s)
		}
	}
	if !found {
		return errors.New("catalog source not found")
	}
	if err := c.saveSources(next); err != nil {
		return err
	}
	c.sources = next
	// Revoking a source cancels its downloads before their install commit.
	for _, j := range c.jobs {
		if j.Source == id && j.State != "installing" {
			j.cancel()
		}
	}
	slog.Info("plugin catalog source removed", "id", id)
	return nil
}
func (c *Catalog) source(id string) (CatalogSource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return CatalogSource{}, errors.New("catalog closed")
	}
	for _, s := range c.sources {
		if s.ID == id {
			return s, nil
		}
	}
	return CatalogSource{}, errors.New("catalog source not configured")
}
func (c *Catalog) fetch(ctx context.Context, location string, limit int64, output io.Writer, progress func(int64, int64)) error {
	u, err := url.Parse(location)
	if err != nil || !githubDownloadURL(u, false) {
		return errors.New("invalid GitHub download location")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", location, nil)
	if err != nil {
		return errors.New("invalid download request")
	}
	req.Header.Set("User-Agent", "NimoteHost-Plugin-Catalog")
	req.Header.Set("Accept", "application/octet-stream")
	res, err := c.client.Do(req)
	if err != nil {
		return errors.New("GitHub download unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("GitHub resource unavailable (missing release, rate limit or server error)")
	}
	if res.ContentLength > limit {
		return errors.New("download exceeds size limit")
	}
	reader := io.LimitReader(res.Body, limit+1)
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return errors.New("download exceeds size limit")
			}
			if _, err = output.Write(buf[:n]); err != nil {
				return err
			}
			if progress != nil {
				progress(total, res.ContentLength)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return errors.New("download interrupted")
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
}
func validateIndex(index CatalogIndex) error {
	if index.Version != "1.0" || index.Plugins == nil || len(index.Plugins) > 256 {
		return errors.New("invalid catalog schema or capacity")
	}
	seen := map[string]bool{}
	for _, p := range index.Plugins {
		repo := strings.Split(p.Repository, "/")
		if !sdk.Name.MatchString(p.ID) || seen[p.ID] || !sdk.Version.MatchString(p.Version) || len(repo) != 2 || !githubPart.MatchString(repo[0]) || !githubPart.MatchString(repo[1]) || !assetName.MatchString(p.Tag) || p.Name == "" || len(p.Name) > 160 || len(p.Description) > 2048 || len(p.Assets) == 0 || len(p.Assets) > 16 {
			return errors.New("invalid catalog entry")
		}
		seen[p.ID] = true
		for platform, a := range p.Assets {
			if !regexp.MustCompile(`^(linux|darwin|windows)/(amd64|arm64|386)$`).MatchString(platform) || !assetName.MatchString(a.Manifest) || !assetName.MatchString(a.Binary) || a.Manifest == a.Binary || !digestPattern.MatchString(a.SHA256) {
				return errors.New("invalid release assets")
			}
		}
	}
	return nil
}
func (c *Catalog) index(ctx context.Context, source CatalogSource) (CatalogIndex, error) {
	var b strings.Builder
	if err := c.fetch(ctx, source.URL, 512<<10, &b, nil); err != nil {
		return CatalogIndex{}, err
	}
	var index CatalogIndex
	dec := json.NewDecoder(strings.NewReader(b.String()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&index); err != nil {
		return index, errors.New("invalid catalog JSON")
	}
	if dec.Decode(new(any)) != io.EOF {
		return index, errors.New("expected one catalog object")
	}
	return index, validateIndex(index)
}
func (c *Catalog) Search(ctx context.Context, query string) CatalogSearch {
	sources := c.Sources()
	result := CatalogSearch{Plugins: []CatalogMatch{}, Failures: []CatalogFailure{}}
	if len(query) > 256 {
		return result
	}
	query = strings.ToLower(strings.TrimSpace(query))
	type part struct {
		s     CatalogSource
		index CatalogIndex
		err   error
	}
	out := make(chan part, len(sources))
	for _, source := range sources {
		go func(s CatalogSource) { index, err := c.index(ctx, s); out <- part{s, index, err} }(source)
	}
	budget := 0
	for range sources {
		p := <-out
		if p.err != nil {
			result.Failures = append(result.Failures, CatalogFailure{Source: p.s.ID, Code: "catalog_unavailable"})
			continue
		}
		for _, entry := range p.index.Plugins {
			if query != "" && !strings.Contains(strings.ToLower(entry.ID+" "+entry.Name+" "+entry.Description+" "+entry.Repository), query) {
				continue
			}
			_, compatible := entry.Assets[runtime.GOOS+"/"+runtime.GOARCH]
			match := CatalogMatch{CatalogEntry: entry, Source: p.s.ID, Compatible: compatible}
			encoded, _ := json.Marshal(match)
			if len(result.Plugins) >= 256 || budget+len(encoded) > 1<<20 {
				result.Truncated = true
				continue
			}
			budget += len(encoded)
			result.Plugins = append(result.Plugins, match)
		}
	}
	sort.Slice(result.Plugins, func(i, j int) bool {
		a, b := result.Plugins[i], result.Plugins[j]
		return a.Source+"/"+a.ID < b.Source+"/"+b.ID
	})
	sort.Slice(result.Failures, func(i, j int) bool { return result.Failures[i].Source < result.Failures[j].Source })
	return result
}
func releaseURL(entry CatalogEntry, asset string) string {
	return "https://github.com/" + entry.Repository + "/releases/download/" + url.PathEscape(entry.Tag) + "/" + url.PathEscape(asset)
}
func (c *Catalog) Offer(ctx context.Context, sourceID, id string) (CatalogOffer, error) {
	var offer CatalogOffer
	source, err := c.source(sourceID)
	if err != nil {
		return offer, err
	}
	index, err := c.index(ctx, source)
	if err != nil {
		return offer, err
	}
	for _, entry := range index.Plugins {
		if entry.ID != id {
			continue
		}
		asset, ok := entry.Assets[runtime.GOOS+"/"+runtime.GOARCH]
		if !ok {
			return offer, errors.New("plugin not available for this platform")
		}
		var b strings.Builder
		if err = c.fetch(ctx, releaseURL(entry, asset.Manifest), 60<<10, &b, nil); err != nil {
			return offer, err
		}
		raw := []byte(b.String())
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != asset.SHA256 {
			return offer, errors.New("release manifest integrity mismatch")
		}
		dec := json.NewDecoder(strings.NewReader(b.String()))
		dec.DisallowUnknownFields()
		var m sdk.Manifest
		if err = dec.Decode(&m); err != nil {
			return offer, errors.New("invalid release manifest")
		}
		if dec.Decode(new(any)) != io.EOF {
			return offer, errors.New("expected one manifest")
		}
		if err = checkManifest(m); err != nil {
			return offer, err
		}
		if m.ID != entry.ID || m.Version != entry.Version {
			return offer, errors.New("catalog/manifest identity mismatch")
		}
		return CatalogOffer{CatalogMatch{CatalogEntry: entry, Source: sourceID, Compatible: true}, m, asset.SHA256, raw}, nil
	}
	return offer, errors.New("plugin not found in catalog")
}

// StartInstall returns before network I/O. Downloaded files are data until
// Runtime.Install atomically publishes a disabled, fully checked package.
func (c *Catalog) StartInstall(r *Runtime, source, id, expected string, update, confirmed bool) (InstallJob, error) {
	if !confirmed || !sdk.Name.MatchString(id) || !digestPattern.MatchString(expected) {
		return InstallJob{}, errors.New("reviewed manifest digest and explicit confirmation required")
	}
	if _, err := c.source(source); err != nil {
		return InstallJob{}, err
	}
	found := false
	for _, p := range r.List() {
		if p.ID == id {
			found = true
			if !update || p.State != "disabled" {
				return InstallJob{}, errors.New("plugin must be disabled before explicit update")
			}
		}
	}
	if update && !found {
		return InstallJob{}, errors.New("plugin not installed")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return InstallJob{}, errors.New("catalog closed")
	}
	active := 0
	for _, job := range c.jobs {
		if job.State == "downloading" || job.State == "installing" {
			if job.Plugin == id {
				return InstallJob{}, errors.New("installation already active for this plugin")
			}
			active++
		}
	}
	if active >= 2 {
		return InstallJob{}, errors.New("download capacity reached")
	}
	if len(c.order) >= 16 {
		found := false
		for i, key := range c.order {
			j := c.jobs[key]
			if j.State != "downloading" && j.State != "installing" {
				delete(c.jobs, key)
				c.order = append(c.order[:i], c.order[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return InstallJob{}, errors.New("install job capacity reached")
		}
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return InstallJob{}, err
	}
	key := hex.EncodeToString(random[:])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	job := &InstallJob{ID: key, Plugin: id, Source: source, State: "downloading", cancel: cancel}
	c.jobs[key] = job
	c.order = append(c.order, key)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		err := c.install(ctx, r, job, expected, update)
		c.mu.Lock()
		defer c.mu.Unlock()
		if err == nil {
			job.State = "succeeded"
			job.Progress = 1
		} else if ctx.Err() != nil {
			job.State = "canceled"
			job.Error = &sdk.Error{Code: "canceled", Message: "download canceled or timed out"}
		} else {
			job.State = "failed"
			job.Error = &sdk.Error{Code: "install_failed", Message: "download, integrity check or installation failed; review catalog and release assets"}
		}
		slog.Info("plugin remote install completed", "id", id, "source", source, "state", job.State)
	}()
	slog.Info("plugin remote install started", "id", id, "source", source)
	return clone(*job), nil
}
func (c *Catalog) install(ctx context.Context, r *Runtime, job *InstallJob, expected string, update bool) error {
	offer, err := c.Offer(ctx, job.Source, job.Plugin)
	if err != nil {
		return err
	}
	if offer.SHA256 != expected {
		return errors.New("catalog changed since review")
	}
	if err = os.MkdirAll(r.dir, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(r.dir, ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), offer.manifestRaw, 0600); err != nil {
		return err
	}
	asset := offer.Entry.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	file, err := os.OpenFile(filepath.Join(dir, offer.Manifest.Entrypoint), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	hash := sha256.New()
	lastProgress := time.Time{}
	var downloaded int64
	err = c.fetch(ctx, releaseURL(offer.Entry.CatalogEntry, asset.Binary), 64<<20, io.MultiWriter(file, hash), func(n, total int64) {
		downloaded = n
		if time.Since(lastProgress) < 100*time.Millisecond {
			return
		}
		lastProgress = time.Now()
		c.mu.Lock()
		job.Bytes = n
		if total <= 0 {
			total = 64 << 20
		}
		job.Progress = min(0.9, float64(n)/float64(total)*0.9)
		c.mu.Unlock()
	})
	c.mu.Lock()
	job.Bytes = downloaded
	c.mu.Unlock()
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != offer.Manifest.SHA256 {
		return errors.New("download integrity mismatch")
	}
	c.mu.Lock()
	configured := false
	for _, s := range c.sources {
		if s.ID == job.Source {
			configured = true
		}
	}
	if ctx.Err() != nil || c.closed || !configured {
		c.mu.Unlock()
		return context.Canceled
	}
	job.State = "installing"
	job.Progress = 0.95
	c.mu.Unlock()
	// Cancellation is no longer accepted once this local commit starts.
	return r.Install(dir, expected, update)
}
func (c *Catalog) Job(id string) (InstallJob, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job := c.jobs[id]
	if job == nil {
		return InstallJob{}, errors.New("install job not found")
	}
	return clone(*job), nil
}
func (c *Catalog) Jobs() []InstallJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []InstallJob{}
	for i := len(c.order) - 1; i >= 0; i-- {
		out = append(out, clone(*c.jobs[c.order[i]]))
	}
	return out
}
func (c *Catalog) CancelInstall(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	job := c.jobs[id]
	if job == nil {
		return errors.New("install job not found")
	}
	if job.State == "installing" {
		return errors.New("local installation commit already started")
	}
	job.cancel()
	return nil
}
func (c *Catalog) Close() {
	c.mu.Lock()
	c.closed = true
	for _, j := range c.jobs {
		j.cancel()
	}
	c.mu.Unlock()
	c.wg.Wait()
	c.client.CloseIdleConnections()
}
