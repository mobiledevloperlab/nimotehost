// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Command nimotehost runs NimoteHost Community over existing authenticated SSH.
package main

import "github.com/mobiledevloperlab/nimotehost/pkg/hostapp"

func main() { hostapp.Main(hostapp.Extension{}) }
