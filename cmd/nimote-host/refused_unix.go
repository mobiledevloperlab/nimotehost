// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

//go:build !windows

package main

import "syscall"

const connectionRefused = syscall.ECONNREFUSED
