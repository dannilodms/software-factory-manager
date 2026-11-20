package main

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// assertOwnerAlone checks that the access list of the path lets the user
// that runs the test alone in, and inherits nothing.
func assertOwnerAlone(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	require.NoError(t, err)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)
	// P: inherits nothing (AI only says the list was written that way). One
	// entry: full access (FA) for the user, which what a folder holds
	// inherits (OICI).
	alone := `^D:P(AI)?\(A;(OICI)?;FA;;;` + regexp.QuoteMeta(user.User.Sid.String()) + `\)$`
	assert.Regexp(t, alone, descriptor.String(), "%s lets in the user that runs the command, and no one else", path)
}
