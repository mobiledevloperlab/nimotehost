// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"unicode/utf16"
)

// Windows uses a per-user interactive scheduled task rather than a privileged
// system service. It runs while the user is logged in, independently of clients.
const hostTaskName = "NimoteHost"

func (s *Service) installWindows(ctx context.Context) error {
	account, err := user.Current()
	if err != nil {
		return err
	}
	path := filepath.Join(s.config.ConfigDir, "nimote-host", "service.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	previous, previousErr := os.ReadFile(path)
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		return previousErr
	}
	document := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger></Triggers>
<Principals><Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>
<Actions Context="Author"><Exec><Command>%s</Command><Arguments>start</Arguments></Exec></Actions></Task>`, xmlEscape(account.Uid), xmlEscape(account.Uid), xmlEscape(s.config.Executable))
	if err := writePrivateFile(path, encodeTaskXML(document)); err != nil {
		return err
	}
	if err := s.run(ctx, "schtasks.exe", "/Create", "/TN", hostTaskName, "/XML", path, "/F"); err != nil {
		// A failed first registration must not masquerade as an installed service:
		// setup relies on this marker to decide whether native cleanup is necessary.
		var restoreErr error
		if previousErr == nil {
			restoreErr = writePrivateFile(path, previous)
		} else {
			restoreErr = os.Remove(path)
		}
		return errors.Join(s.managerError("register task", err), restoreErr)
	}
	if err := s.run(ctx, "schtasks.exe", "/Run", "/TN", hostTaskName); err != nil {
		return s.managerError("start task", err)
	}
	return nil
}
func (s *Service) uninstallWindows(ctx context.Context) error {
	path := filepath.Join(s.config.ConfigDir, "nimote-host", "service.xml")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := s.run(ctx, "schtasks.exe", "/Delete", "/TN", hostTaskName, "/F"); err != nil {
		return err
	}
	return os.Remove(path)
}
func (s *Service) restartWindows(ctx context.Context) error {
	// The caller stops Host gracefully first; /Run then starts the registered task.
	if err := s.run(ctx, "schtasks.exe", "/Run", "/TN", hostTaskName); err != nil {
		return s.managerError("start task", err)
	}
	return nil
}

// schtasks imports XML as UTF-16. A matching declaration and BOM are required;
// UTF-8 input with an explicit encoding declaration is rejected by Windows.
func encodeTaskXML(document string) []byte {
	units := utf16.Encode([]rune(document))
	data := make([]byte, 2+2*len(units))
	data[0], data[1] = 0xff, 0xfe
	for index, unit := range units {
		data[2+2*index] = byte(unit)
		data[3+2*index] = byte(unit >> 8)
	}
	return data
}
