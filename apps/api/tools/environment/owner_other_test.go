//go:build !windows

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertOwnerAlone checks that the mode lets the owner alone reach the path.
func assertOwnerAlone(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o077, "%s is reached by its owner alone, not %v", path, info.Mode().Perm())
}
