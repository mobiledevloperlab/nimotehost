// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Devices are deliberately read-only. Giving a same-user client command execution
// would let it read the owner's credential and bypass device revocation.
type Device struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Role    string     `json:"role"`
	Created time.Time  `json:"created"`
	Revoked *time.Time `json:"revoked,omitempty"`
}
type deviceRecord struct {
	Device
	Hash string `json:"credential_hash"`
}
type deviceContextKey struct{}

func DeviceContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, deviceContextKey{}, id)
}
func deviceDir(dir string) (string, error) {
	if err := privateDir(dir); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "devices")
	return p, privateDir(p)
}
func readPrivate(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("expected private regular file")
	}
	if err = secureFile(path, info); err != nil {
		return nil, err
	}
	if info.Size() > 16384 {
		return nil, errors.New("device record too large")
	}
	return os.ReadFile(path)
}
func readDevice(dir, id string) (deviceRecord, error) {
	var r deviceRecord
	if !validID(id) {
		return r, errors.New("invalid device ID")
	}
	p, err := deviceDir(dir)
	if err != nil {
		return r, err
	}
	b, err := readPrivate(filepath.Join(p, id+".json"))
	if err != nil {
		return r, err
	}
	if err = decodeJSON(b, &r); err != nil {
		return r, err
	}
	if r.ID != id || r.Role != "observer" || len(r.Hash) != 64 {
		return r, errors.New("invalid device record")
	}
	return r, nil
}
func CreateDevice(dir, name string) (Device, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") {
		return Device{}, errors.New("device name must be 1–128 bytes without control lines")
	}
	lock, err := ControlLock(dir)
	if err != nil {
		return Device{}, err
	}
	defer lock.Close()
	p, err := deviceDir(dir)
	if err != nil {
		return Device{}, err
	}
	id, err := randomID()
	if err != nil {
		return Device{}, err
	}
	secret, err := randomID()
	if err != nil {
		return Device{}, err
	}
	credential := "device." + id + "." + secret
	digest := sha256.Sum256([]byte(credential))
	d := Device{ID: id, Name: name, Role: "observer", Created: time.Now().UTC()}
	if err = atomicWrite(filepath.Join(p, id+".credential"), []byte(credential)); err != nil {
		return Device{}, err
	}
	if err = saveJSON(filepath.Join(p, id+".json"), deviceRecord{d, hex.EncodeToString(digest[:])}); err != nil {
		_ = os.Remove(filepath.Join(p, id+".credential"))
		return Device{}, err
	}
	return d, nil
}
func ListDevices(dir string) ([]Device, error) {
	p, err := deviceDir(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	result := []Device{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			r, err := readDevice(dir, strings.TrimSuffix(e.Name(), ".json"))
			if err != nil {
				return nil, err
			}
			result = append(result, r.Device)
		}
	}
	return result, nil
}
func RevokeDevice(dir, id string) error {
	lock, err := ControlLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	r, err := readDevice(dir, id)
	if err != nil {
		return err
	}
	if r.Revoked != nil {
		return nil
	}
	now := time.Now().UTC()
	r.Revoked = &now
	return saveJSON(filepath.Join(dir, "devices", id+".json"), r)
}
func deviceCredential(ctx context.Context, dir string) (string, bool, error) {
	id, ok := ctx.Value(deviceContextKey{}).(string)
	if !ok {
		return "", false, nil
	}
	r, err := readDevice(dir, id)
	if err != nil {
		return "", true, err
	}
	if r.Revoked != nil {
		return "", true, errors.New("device authorization revoked")
	}
	b, err := readPrivate(filepath.Join(dir, "devices", id+".credential"))
	return string(b), true, err
}
func authenticateDevice(dir, credential string) bool {
	parts := strings.Split(credential, ".")
	if len(parts) != 3 || parts[0] != "device" || !validID(parts[1]) || !validID(parts[2]) {
		return false
	}
	r, err := readDevice(dir, parts[1])
	if err != nil || r.Revoked != nil {
		return false
	}
	digest := sha256.Sum256([]byte(credential))
	hash, err := hex.DecodeString(r.Hash)
	return err == nil && subtle.ConstantTimeCompare(digest[:], hash) == 1
}
func observerAllowed(method, path string) bool {
	if method != "GET" {
		return false
	}
	if path == "/v1/status" || path == "/v1/capabilities" || path == "/v1/sessions" || path == "/v1/events" {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(path, "/v1/sessions/"), "/")
	return strings.HasPrefix(path, "/v1/sessions/") && len(parts) <= 2 && validID(parts[0]) && (len(parts) == 1 || parts[1] == "output" || parts[1] == "activity" || parts[1] == "agent-state")
}

// DeviceSSHCommand generates the forced command only. Administrators bind it to
// a unique public key with OpenSSH restrict,command="..."; never a normal shell key.
func DeviceSSHCommand(binary, id string) (string, error) {
	if !filepath.IsAbs(binary) || !validID(id) || strings.ContainsAny(binary, "\x00\r\n") {
		return "", errors.New("invalid CLI path or device ID")
	}
	// POSIX shell quoting; Windows OpenSSH must use its configured shell's quoting.
	return fmt.Sprintf("'%s' device-bridge %s", strings.ReplaceAll(binary, "'", "'\"'\"'"), id), nil
}
