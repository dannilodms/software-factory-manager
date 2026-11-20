package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheLocalCopyOfTheKeysIsReachedByItsOwnerAlone(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	for _, path := range []string{
		world.localCopy(),
		filepath.Join(world.localCopy(), "deploy-key"),
		filepath.Join(world.localCopy(), "known_hosts"),
		filepath.Join(world.localCopy(), "ssh_config"),
	} {
		assertOwnerAlone(t, path)
	}
}

func TestOpenSSHAcceptsTheLocalCopyOfTheDeployKey(t *testing.T) {
	t.Parallel()
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("OpenSSH is not installed here")
	}
	world := aWorld(t)
	world.succeeds("on", "staging")

	// OpenSSH refuses a private key that another account can read, on Linux,
	// macOS and Windows alike, before it reads the key.
	public, err := exec.Command(keygen, "-y", "-f", filepath.Join(world.localCopy(), "deploy-key")).CombinedOutput()

	require.NoError(t, err, string(public))
	assert.Equal(t, world.specification().DeployPublicKey, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(public)), "deploy@staging")))
}

func TestARemovedLocalCopyLeavesNothing(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")

	world.succeeds("off", "staging")

	entries, err := os.ReadDir(filepath.Join(world.directory, ".servers"))
	require.NoError(t, err)
	assert.Empty(t, entries)
}
