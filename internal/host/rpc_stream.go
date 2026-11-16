// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// ServeRPCStream carries bounded, multiplexed requests over one SSH exec
// channel. The credential stays on the host. EOF only detaches the client;
// processes belong to the daemon, never to this bridge.
func ServeRPCStream(ctx context.Context, dir string, input io.Reader, output io.Writer) error {
	return serveRPCStream(ctx, input, output, func(ctx context.Context, request RPCRequest) ([]byte, error) { return Call(ctx, dir, request) })
}

func serveRPCStream(ctx context.Context, input io.Reader, output io.Writer, call func(context.Context, RPCRequest) ([]byte, error)) (resultErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// SSH stdout can fill if the reader disappears without closing stdin.
	// Closeable writers (including os.Stdout) must release blocked encoders.
	if closer, ok := output.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	// A cancelled SSH bridge must stop even if the peer keeps stdin open.
	// Closing the reader unblocks Scanner.Scan on the CLI's os.Stdin.
	if closer, ok := input.(io.Closer); ok {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	var mu sync.Mutex
	var writeErr error
	encoder := json.NewEncoder(output)
	write := func(value any) error {
		mu.Lock()
		defer mu.Unlock()
		err := encoder.Encode(value)
		if err != nil {
			if writeErr == nil {
				writeErr = err
			}
			cancel()
		}
		return err
	}
	if err := write(map[string]any{"stream_protocol": 1}); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxRequest+1024)
	slots := make(chan struct{}, 8)
	var idsMu sync.Mutex
	inflight := make(map[uint64]bool)
	var workers sync.WaitGroup
	// On malformed input or cancellation, stop calls before joining them.
	// A clean EOF still drains accepted calls below.
	defer func() {
		cancel()
		workers.Wait()
		mu.Lock()
		defer mu.Unlock()
		// A failed response cancels the stream. Preserve the original write
		// failure even when cancellation wins a race with the input loop.
		if writeErr != nil && !errors.Is(resultErr, writeErr) {
			resultErr = errors.Join(resultErr, writeErr)
		}
	}()
	for scanner.Scan() {
		var frame struct {
			ID uint64 `json:"id"`
			RPCRequest
		}
		if err := decodeJSON(scanner.Bytes(), &frame); err != nil || frame.ID == 0 {
			return io.ErrUnexpectedEOF
		}
		idsMu.Lock()
		duplicate := inflight[frame.ID]
		if !duplicate {
			inflight[frame.ID] = true
		}
		idsMu.Unlock()
		if duplicate {
			return errors.New("duplicate in-flight RPC stream id")
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		workers.Add(1)
		go func(id uint64, request RPCRequest) {
			defer workers.Done()
			defer func() { idsMu.Lock(); delete(inflight, id); idsMu.Unlock() }()
			defer func() { <-slots }()
			result, err := call(ctx, request)
			if err != nil {
				_ = write(map[string]any{"id": id, "error": err.Error()})
			} else {
				_ = write(map[string]any{"id": id, "result": json.RawMessage(result)})
			}
		}(frame.ID, frame.RPCRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		cancel()
		return err
	}
	workers.Wait()
	mu.Lock()
	defer mu.Unlock()
	if writeErr != nil {
		return writeErr
	}
	return ctx.Err()
}
