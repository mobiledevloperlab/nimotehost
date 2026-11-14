// SPDX-License-Identifier: Apache-2.0
//go:build darwin || windows

package host

import (
	"errors"
	"os/exec"
)

func validateExecutionIdentity(identity *ExecutionIdentity, dir string, l ResourceLimits) error {
	if identity != nil {
		return errors.New("independent execution identities are unsupported on this platform")
	}
	return nil
}
func identityEnvironment(identity *ExecutionIdentity, dir string) []string { return nil }
func configureIdentityChild(cmd *exec.Cmd, identity *ExecutionIdentity) error {
	if identity != nil {
		return errors.New("independent execution identities are unsupported on this platform")
	}
	return nil
}

func verifyUnusedExecutionUID(uid uint32) error {
	return errors.New("independent execution identities are unsupported on this platform")
}
