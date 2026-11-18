// SPDX-License-Identifier: Apache-2.0
// Command host-client demonstrates read-only Host negotiation and session queries.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mobiledevloperlab/nimotehost/pkg/hostclient"
)

func main() {
	remote := flag.String("ssh", "", "SSH destination; empty uses the local Host")
	flag.Parse()
	transport := hostclient.CommandTransport("nimotehost", "rpc-stream")
	if *remote != "" {
		transport = hostclient.CommandTransport("ssh", "--", *remote, "nimotehost", "rpc-stream")
	}
	client := hostclient.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := inspect(ctx, &client); err != nil {
		var rejection *hostclient.Error
		if errors.As(err, &rejection) {
			fmt.Fprintf(os.Stderr, "Host error: status=%d code=%s\n", rejection.Status, rejection.Code)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func inspect(ctx context.Context, client *hostclient.Client) error {
	caps, err := client.Capabilities(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Host %s, version %s, protocol %d\n", caps.HostID, caps.Version, caps.Protocol)
	var sessions json.RawMessage
	if err = client.Do(ctx, "GET", "/v1/sessions", nil, &sessions); err != nil {
		return err
	}
	_, err = fmt.Println(string(sessions))
	return err
}
