// SPDX-License-Identifier: Apache-2.0
//go:build !darwin && !windows && !linux

package main

import "runtime"

func platformArch() string { return runtime.GOARCH }
