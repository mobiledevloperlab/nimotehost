// SPDX-License-Identifier: Apache-2.0
package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

// Advanced Linux-only execution boundary. A fresh dedicated numeric UID is
// reserved for one session forever; it is not the Host's account or another
// session's identity. Unsupported/nonprivileged backends fail before execution.
type ExecutionIdentity struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}
type sessionScope struct {
	Identity   ExecutionIdentity `json:"identity"`
	CgroupRoot string            `json:"cgroup_root"`
	Name       string            `json:"name"`
	SessionID  string            `json:"session_id"`
}

func scopeName(dir, id string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(dir)))
	return "nimotehost-" + hex.EncodeToString(hash[:8]) + "-" + id
}
func loadSessionScope(dir, id string) (*sessionScope, error) {
	if !validID(id) {
		return nil, errors.New("invalid scoped session ID")
	}
	b, err := readPrivateLimit(filepath.Join(dir, id+".scope"), 16<<10)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var scope sessionScope
	if decodeJSON(b, &scope) != nil || scope.SessionID != id || scope.Name != scopeName(dir, id) || scope.Identity.UID < 1000000 || scope.Identity.UID > 2147483646 || scope.Identity.GID != scope.Identity.UID {
		return nil, errors.New("invalid execution scope metadata")
	}
	return &scope, nil
}
func reserveExecutionIdentity(dir, id string, identity ExecutionIdentity) error {
	if err := verifyUnusedExecutionUID(identity.UID); err != nil {
		return err
	}
	root := filepath.Join(filepath.Dir(dir), "identities")
	if err := privateDir(root); err != nil {
		return err
	}
	// Keep reservations after deletion/revocation: reuse could expose abandoned
	// workspace files or credentials owned by that numeric UID.
	p := filepath.Join(root, strconv.FormatUint(uint64(identity.UID), 10))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("execution UID is already reserved or cannot be reserved")
	}
	_, writeErr := f.WriteString(id)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return syncDirectory(root)
}
func controllerBinding(dir string, uid uint32) (string, error) {
	data, err := readPrivate(filepath.Join(dir, "controllers", strconv.FormatUint(uint64(uid), 10)))
	if err != nil {
		return "", err
	}
	id := string(data)
	if !validID(id) {
		return "", errors.New("invalid controller binding")
	}
	return id, nil
}
func (m *Manager) controllerSessionAllowed(d Device, id string) bool {
	m.mu.Lock()
	item := m.sessions[id]
	var scope *sessionScope
	directory := ""
	if item != nil {
		scope = item.scope
		directory = item.session.Directory
	}
	m.mu.Unlock()
	if scope == nil || d.IdentityScopes[id] != scope.Identity.UID || m.limits.CgroupRoot != scope.CgroupRoot || validateExecutionIdentity(&scope.Identity, directory, m.limits) != nil {
		return false
	}
	binding, err := controllerBinding(filepath.Dir(m.dir), scope.Identity.UID)
	return err == nil && binding == d.ID
}
func (m *Manager) controllerRevoked(scope *sessionScope) bool {
	if scope == nil {
		return false
	}
	dir := filepath.Dir(m.dir)
	id, err := controllerBinding(dir, scope.Identity.UID)
	if errors.Is(err, os.ErrNotExist) {
		return false
	} // Owner has not delegated it.
	if err != nil {
		return true
	}
	record, err := readDevice(dir, id)
	return err != nil || record.Revoked != nil || record.IdentityScopes[scope.SessionID] != scope.Identity.UID
}

func CreateControllerDevice(dir, name string, sessions []string) (Device, error) {
	if len(sessions) == 0 || len(sessions) > 64 {
		return Device{}, errors.New("controller requires 1..64 isolated sessions")
	}
	return createDevice(dir, name, "session_controller", sessions)
}
func validateControllerScopes(dir string, sessions []string) (map[string]uint32, error) {
	if len(sessions) == 0 || len(sessions) > 64 {
		return nil, errors.New("controller requires 1..64 isolated sessions")
	}
	identities := make(map[string]uint32, len(sessions))
	if err := privateDir(filepath.Join(dir, "controllers")); err != nil {
		return nil, err
	}
	for _, id := range sessions {
		scope, err := loadSessionScope(filepath.Join(dir, "sessions"), id)
		if err != nil || scope == nil {
			return nil, errors.New("controller sessions require verified independent execution identities")
		}
		raw, err := readPrivateLimit(filepath.Join(dir, "sessions", id+".json"), 2<<20)
		if err != nil {
			return nil, err
		}
		var record Session
		if decodeJSON(raw, &record) != nil || record.ID != id {
			return nil, errors.New("controller session metadata is unavailable")
		}
		limits := DefaultResourceLimits()
		limits.CgroupRoot = scope.CgroupRoot
		if err := validateExecutionIdentity(&scope.Identity, record.Directory, limits); err != nil {
			return nil, err
		}
		reserved, err := readPrivate(filepath.Join(dir, "identities", strconv.FormatUint(uint64(scope.Identity.UID), 10)))
		if err != nil || string(reserved) != id {
			return nil, errors.New("execution identity reservation does not match session")
		}
		if _, err := controllerBinding(dir, scope.Identity.UID); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("execution identity has already been assigned to a controller")
		}
		identities[id] = scope.Identity.UID
	}
	return identities, nil
}
