// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

// Desktop DTOs follow public HCP JSON, independent of runtime implementation types.
type PluginInfo struct {
	ID       string       `json:"id"`
	State    string       `json:"state"`
	Manifest sdk.Manifest `json:"manifest"`
	Grants   []string     `json:"grants"`
}
type PluginCapability struct {
	sdk.Capability
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	State  string `json:"state"`
}
type PluginRequest struct {
	Protocol  string          `json:"protocol"`
	Operation string          `json:"operation"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Confirmed bool            `json:"confirmed,omitempty"`
	Grants    []string        `json:"grants,omitempty"`
	Source    string          `json:"source,omitempty"`
	SHA256    string          `json:"sha256,omitempty"`
	Epoch     string          `json:"epoch,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Query     string          `json:"query,omitempty"`
	After     uint64          `json:"after,omitempty"`
}

func (m *Manager) PluginCall(ctx context.Context, req PluginRequest) (json.RawMessage, error) {
	req.Protocol = sdk.Protocol
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	raw, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "POST", Path: "/v1/extensions", Body: body})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Protocol string          `json:"protocol"`
		Result   json.RawMessage `json:"result"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Protocol != sdk.Protocol || !json.Valid(envelope.Result) {
		return nil, errors.New("incompatible capability response")
	}
	return envelope.Result, nil
}
func PluginRequestID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ReviewPluginBundle pins exact manifest bytes for the later confirmed install.
// Reading a manifest never executes a plugin or an install hook.
func ReviewPluginBundle(dir string) (sdk.Manifest, string, error) {
	var manifest sdk.Manifest
	if !filepath.IsAbs(dir) {
		return manifest, "", errors.New("absolute bundle path required")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return manifest, "", errors.New("invalid bundle directory")
	}
	path := filepath.Join(dir, "manifest.json")
	info, err = os.Lstat(path)
	if err != nil {
		return manifest, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 60<<10 {
		return manifest, "", errors.New("invalid manifest file")
	}
	file, err := os.Open(path)
	if err != nil {
		return manifest, "", err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, (60<<10)+1))
	if err != nil {
		return manifest, "", err
	}
	if len(b) > 60<<10 {
		return manifest, "", errors.New("manifest too large")
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		return manifest, "", err
	}
	if err = manifest.Validate(); err != nil {
		return manifest, "", err
	}
	sum := sha256.Sum256(b)
	return manifest, hex.EncodeToString(sum[:]), nil
}
