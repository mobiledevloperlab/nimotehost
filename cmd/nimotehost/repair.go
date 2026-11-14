// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"errors"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"io"
)

func repairCommand(args []string, out io.Writer) error {
	dir, err := host.DefaultDir()
	if err != nil {
		return err
	}
	if err := host.CheckStateName(dir); err != nil {
		return err
	}
	var result any
	switch {
	case len(args) == 1 && args[0] == "inspect":
		result, err = host.InspectRecovery(dir)
	case len(args) == 2 && args[0] == "apply":
		result, err = host.ApplyRecovery(dir, args[1])
	default:
		return errors.New("usage: nimotehost repair inspect | repair apply <plan-id>; Host must be stopped")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
