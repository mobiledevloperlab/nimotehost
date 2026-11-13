// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package main

import "golang.org/x/sys/windows"

// Winsock errors differ from the POSIX-style syscall.ECONNREFUSED constant.
const connectionRefused = windows.WSAECONNREFUSED
