package secret

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Restrict gives path an access control list that names the current user alone
// and inherits nothing. Windows has no mode bits, so mode is unused.
func Restrict(path string, _ os.FileMode) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("restrict %s: %w", path, err)
	}

	// D:P is a protected list, which takes nothing from the parent. The one entry
	// allows (A) full access (FA) to the user, and OICI passes it on to what a
	// directory holds.
	descriptor, err := windows.SecurityDescriptorFromString(
		"D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")",
	)
	if err != nil {
		return fmt.Errorf("restrict %s: %w", path, err)
	}

	list, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("restrict %s: %w", path, err)
	}

	err = windows.SetNamedSecurityInfo(
		path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, list, nil,
	)
	if err != nil {
		return fmt.Errorf("restrict %s: %w", path, err)
	}

	return nil
}
