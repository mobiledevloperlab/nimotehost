//go:build !windows

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors
package host

import "syscall"

const rpcConnectionRefused = syscall.ECONNREFUSED
