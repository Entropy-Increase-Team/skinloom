//go:build windows

package fsperm

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func private(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	// Already private: by an earlier run, or by an administrator, who may have added the
	// service account (skinloom service install grants LOCAL SERVICE explicitly). Setting
	// the list again would drop that account when an administrator runs a command.
	if sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION); err == nil {
		if control, _, err := sd.Control(); err == nil && control&windows.SE_DACL_PROTECTED != 0 {
			return nil
		}
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("fsperm: current user: %w", err)
	}
	// P: protected, nothing inherited from the folder above. FA: full access.
	// OICI: files and folders created inside inherit the entry.
	inherit := ""
	if info.IsDir() {
		inherit = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%[1]s;FA;;;%[2]s)(A;%[1]s;FA;;;SY)(A;%[1]s;FA;;;BA)", inherit, user.User.Sid.String())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("fsperm: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("fsperm: %w", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	switch {
	case errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION):
		return nil // FAT and the like have no permissions to set
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		// Made by another account (the site was tried interactively before it became a
		// service): this one may use it but not change who else may. It stays as it was.
		return nil
	}
	if err != nil {
		return fmt.Errorf("fsperm: restrict %s: %w", path, err)
	}
	return nil
}
