package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// restrictToOwner lets the user that runs the command alone reach a file or
// a folder. The modes of Go set no access list on Windows, and OpenSSH for
// Windows refuses a private key that another account can read: the file
// gets an access list of its own, with the user alone, and inherits none.
// What a folder holds inherits its list.
func restrictToOwner(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	inheritance := ""
	if info.IsDir() {
		inheritance = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;" + inheritance + ";FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	list, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, list, nil)
}
