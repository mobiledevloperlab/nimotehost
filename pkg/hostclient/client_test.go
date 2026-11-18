// SPDX-License-Identifier: Apache-2.0
package hostclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCapabilitiesNegotiation(t *testing.T) {
	for _, raw := range []string{
		`{"protocol":1,"min_protocol":1,"host_id":"one","features":{"new_feature":true},"future":{}}`,
		`{"protocol":2,"min_protocol":2,"host_id":"one","features":{}}`,
		`{"protocol":1,"min_protocol":1,"host_id":"one"}`,
	} {
		c := Client{Transport: func(context.Context, Request) (json.RawMessage, error) { return json.RawMessage(raw), nil }}
		caps, err := c.Capabilities(context.Background())
		if strings.Contains(raw, "new_feature") {
			if err != nil || !caps.Supports("new_feature") || caps.Supports("absent") {
				t.Fatalf("%+v %v", caps, err)
			}
		} else if err == nil {
			t.Fatal("accepted incompatible capabilities")
		}
	}
}

func TestCommandTransportHelper(t *testing.T) {
	if os.Getenv("NIMOTE_CLIENT_TEST_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(2)
	}
	var req struct {
		ID int `json:"id"`
		Request
	}
	if json.Unmarshal(scanner.Bytes(), &req) != nil || req.Path != "/v1/status" {
		os.Exit(3)
	}
	mode := os.Args[len(os.Args)-1]
	fmt.Println(`{"stream_protocol":1}`)
	switch mode {
	case "success":
		fmt.Println(`{"id":1,"result":{"ok":true,"future":1}}`)
	case "structured":
		fmt.Println(`{"id":1,"error":"legacy rejection","error_detail":{"status":403,"code":"permission_denied","message":"denied"}}`)
	case "legacy":
		fmt.Println(`{"id":1,"error":"old host rejection"}`)
	case "oversized":
		fmt.Println(strings.Repeat("x", (2<<20)+1))
	case "invalid":
		fmt.Println(`{"id":2,"result":{}}`)
	case "hang":
		time.Sleep(time.Hour)
	}
	os.Exit(0)
}

func TestCommandTransportBoundaries(t *testing.T) {
	t.Setenv("NIMOTE_CLIENT_TEST_HELPER", "1")
	for _, mode := range []string{"success", "structured", "legacy", "oversized", "invalid", "hang"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "hang" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 100*time.Millisecond)
				defer stop()
			}
			c := Client{Transport: CommandTransport(os.Args[0], "-test.run=^TestCommandTransportHelper$", "--", mode)}
			var result struct {
				OK bool `json:"ok"`
			}
			err := c.Do(ctx, "GET", "/v1/status", nil, &result)
			switch mode {
			case "success":
				if err != nil || !result.OK {
					t.Fatalf("%+v %v", result, err)
				}
			case "structured":
				var e *Error
				if !errors.As(err, &e) || e.Status != 403 || e.Code != "permission_denied" || e.Error() != "legacy rejection" {
					t.Fatalf("%v", err)
				}
			case "legacy":
				var e *Error
				if !errors.As(err, &e) || e.Code != "unknown" {
					t.Fatalf("%v", err)
				}
			case "hang":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected deadline, got %v", err)
				}
			default:
				if err == nil {
					t.Fatal("accepted bad response")
				}
			}
		})
	}
}

func TestTransportDoesNotRetryMutations(t *testing.T) {
	calls := 0
	c := Client{Transport: func(context.Context, Request) (json.RawMessage, error) {
		calls++
		return nil, errors.New("response lost")
	}}
	if c.Do(context.Background(), "POST", "/v1/sessions", map[string]string{"kind": "task"}, nil) == nil || calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
