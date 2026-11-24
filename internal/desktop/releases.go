// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"runtime"

	"github.com/mobiledevloperlab/nimotehost/internal/hostinstall"
)

const ReleasesURL = "https://github.com/mobiledevloperlab/nimotehost/releases"

type Release struct {
	Version string
	URL     string
}

// LatestRelease is explicit user-initiated metadata lookup, never executable download.
func LatestRelease(ctx context.Context) (Release, error) {
	tag, _, err := hostinstall.ResolveRelease(ctx, "", runtime.GOOS, runtime.GOARCH, true)
	if err != nil {
		return Release{}, err
	}
	return Release{Version: hostinstall.CommunityVersion(tag), URL: ReleasesURL + "/tag/" + tag}, nil
}
