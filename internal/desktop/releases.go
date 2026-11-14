// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const ReleasesURL = "https://github.com/mobiledevloperlab/nimotehost/releases"

type Release struct {
	Version string `json:"tag_name"`
	URL     string `json:"html_url"`
}

// LatestRelease is explicit user-initiated metadata lookup, never executable download.
func LatestRelease(ctx context.Context) (Release, error) {
	return latestRelease(ctx, &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, "https://api.github.com/repos/mobiledevloperlab/nimotehost/releases/latest")
}
func latestRelease(ctx context.Context, client *http.Client, endpoint string) (Release, error) {
	var release Release
	r, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return release, err
	}
	r.Header.Set("Accept", "application/vnd.github+json")
	r.Header.Set("User-Agent", "Nimote-Host-Desktop")
	response, err := client.Do(r)
	if err != nil {
		return release, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return release, errors.New("release lookup unavailable (no published release, rate limit or server error)")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil {
		return release, err
	}
	if len(raw) > 262144 {
		return release, errors.New("release metadata too large")
	}
	if err = json.Unmarshal(raw, &release); err != nil {
		return release, err
	}
	if release.Version == "" || !strings.HasPrefix(release.URL, ReleasesURL+"/tag/") {
		return Release{}, errors.New("invalid release metadata")
	}
	return release, nil
}
