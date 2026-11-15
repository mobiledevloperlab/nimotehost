// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"os"
)

func main() {
	if sdk.Serve(context.Background(), os.Stdin, os.Stdout, func(ctx context.Context, name string, input json.RawMessage, progress func(float64)) (any, error) {
		if name != "echo" {
			return nil, errors.New("unknown capability")
		}
		var value map[string]any
		if err := json.Unmarshal(input, &value); err != nil {
			return nil, err
		}
		return value, nil
	}) != nil {
		os.Exit(1)
	}
}
