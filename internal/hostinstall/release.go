// SPDX-License-Identifier: Apache-2.0
package hostinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const repository = "mobiledevloperlab/nimotehost"
const maxArchive = 256 << 20

var stableTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var (
	ErrUnsupported = errors.New("unsupported platform")
	ErrRelease     = errors.New("release unavailable")
	ErrIntegrity   = errors.New("archive verification failed")
)

type ReleaseAsset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	State  string `json:"state"`
}

type releaseMetadata struct {
	Tag        string         `json:"tag_name"`
	Prerelease bool           `json:"prerelease"`
	Draft      bool           `json:"draft"`
	Assets     []ReleaseAsset `json:"assets"`
}

func Supported(goos, arch string) bool {
	return (goos == "linux" || goos == "darwin" || goos == "windows") &&
		(arch == "amd64" || arch == "arm64" || arch == "386" && goos != "darwin")
}

func releaseClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("invalid release redirect")
		}
		switch req.URL.Host {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			if via[0].URL.Host == "github.com" {
				return nil
			}
		}
		return errors.New("unexpected release redirect")
	}}
}

func ResolveRelease(ctx context.Context, tag, goos, arch string, desktop bool) (string, ReleaseAsset, error) {
	if !Supported(goos, arch) || desktop && arch == "386" {
		return "", ReleaseAsset{}, fmt.Errorf("%w: %s/%s", ErrUnsupported, goos, arch)
	}
	endpoint := "https://api.github.com/repos/" + repository + "/releases/latest"
	if tag != "" {
		if !stableTag.MatchString(tag) {
			return "", ReleaseAsset{}, fmt.Errorf("%w: expected a stable vMAJOR.MINOR.PATCH tag", ErrRelease)
		}
		endpoint = "https://api.github.com/repos/" + repository + "/releases/tags/" + tag
	}
	var release releaseMetadata
	err := fetch(ctx, releaseClient(), endpoint, func(r io.Reader) error {
		data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return fmt.Errorf("%w: metadata exceeds limit", ErrRelease)
		}
		if err := json.Unmarshal(data, &release); err != nil {
			return fmt.Errorf("%w: invalid metadata", ErrRelease)
		}
		return nil
	})
	if err != nil {
		return "", ReleaseAsset{}, err
	}
	asset, err := release.selectAsset(tag, goos, arch, desktop)
	return release.Tag, asset, err
}

func (r releaseMetadata) selectAsset(tag, goos, arch string, desktop bool) (ReleaseAsset, error) {
	if r.Draft || r.Prerelease || !stableTag.MatchString(r.Tag) || tag != "" && tag != r.Tag {
		return ReleaseAsset{}, fmt.Errorf("%w: expected a published stable release", ErrRelease)
	}
	prefix := "nimotehost-"
	if desktop {
		prefix += "gui-"
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	name := prefix + r.Tag[1:] + "-" + goos + "-" + arch + ext
	for _, asset := range r.Assets {
		if asset.Name != name {
			continue
		}
		if asset.State != "uploaded" || asset.Size < 1 || asset.Size > maxArchive ||
			asset.URL != "https://github.com/"+repository+"/releases/download/"+r.Tag+"/"+name {
			return ReleaseAsset{}, fmt.Errorf("%w: invalid asset metadata for %s", ErrRelease, name)
		}
		if !strings.HasPrefix(asset.Digest, "sha256:") || !validDigest(strings.TrimPrefix(asset.Digest, "sha256:")) {
			return ReleaseAsset{}, fmt.Errorf("%w: GitHub SHA-256 missing for %s", ErrIntegrity, name)
		}
		return asset, nil
	}
	return ReleaseAsset{}, fmt.Errorf("%w: %s is missing", ErrRelease, name)
}

func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

func (a ReleaseAsset) Download(ctx context.Context, filename string) error {
	return a.download(ctx, releaseClient(), filename)
}

func (a ReleaseAsset) download(ctx context.Context, client *http.Client, filename string) error {
	return fetch(ctx, client, a.URL, func(r io.Reader) (result error) {
		file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer func() {
			result = errors.Join(result, file.Close())
			if result != nil {
				_ = os.Remove(filename)
			}
		}()
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(r, a.Size+1))
		if err != nil {
			return err
		}
		if n < a.Size {
			return fmt.Errorf("%w: truncated asset: %w", ErrRelease, io.ErrUnexpectedEOF)
		}
		if n != a.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != a.Digest {
			return fmt.Errorf("%w: %s", ErrIntegrity, a.Name)
		}
		return nil
	})
}

func fetch(ctx context.Context, client *http.Client, url string, consume func(io.Reader) error) error {
	const attempts = 3
	var err error
	for attempt := range attempts {
		var retry bool
		retry, err = fetchOnce(ctx, client, url, consume)
		if err == nil || !retry || attempt == attempts-1 {
			return err
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func fetchOnce(ctx context.Context, client *http.Client, url string, consume func(io.Reader) error) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "NimoteHost-Deployment")
	if req.URL.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := client.Do(req)
	if err != nil {
		// Transport errors may contain a temporary signed asset URL.
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("%w: HTTPS request failed", ErrRelease)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode >= 500 || resp.StatusCode == 429,
			fmt.Errorf("%w: HTTP %d", ErrRelease, resp.StatusCode)
	}
	err = consume(resp.Body)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = errors.Join(ErrRelease, err)
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return errors.Is(err, io.ErrUnexpectedEOF), err
}
