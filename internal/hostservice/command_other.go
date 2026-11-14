//go:build !windows

// SPDX-License-Identifier: Apache-2.0
package hostservice

import "os/exec"

func hideManagerWindow(*exec.Cmd) {}
