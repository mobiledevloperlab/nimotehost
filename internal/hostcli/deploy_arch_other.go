// SPDX-License-Identifier: Apache-2.0
//go:build !darwin && !windows && !linux

package hostcli

import "runtime"

func platformArch() string { return runtime.GOARCH }
