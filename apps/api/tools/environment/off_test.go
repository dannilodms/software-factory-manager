package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTurningStagingOffDestroysItsServerAndRemovesTheLocalCopyOfTheKey(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")

	printed := world.succeeds("off", "staging")

	assert.Empty(t, world.provider.Servers())
	assert.NoDirExists(t, world.localCopy())
	assert.Equal(t, "Staging is off: the server staging at 203.0.113.10 was destroyed at hetzner, with its address and its deploy key.\nThe GitHub Environment staging keeps only the password of Mailpit.\n", printed)
}

func TestTurningOffWhenOffSucceedsAndSaysSo(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	printed := world.succeeds("off", "staging")

	assert.Equal(t, "Staging was off already: there was no server to destroy.\nThe GitHub Environment staging keeps only the password of Mailpit.\n", printed)
}

func TestATurnOffThatCannotDestroyTheServerFailsAndKeepsTheLocalCopy(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.provider.FailDestroyingWith(errors.New("the provider does not answer"))

	ran := world.run("off", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: destroy the server staging at hetzner: the provider does not answer\n", ran.stderr)
	assert.Len(t, world.provider.Servers(), 1)
	assert.DirExists(t, world.localCopy(), "the server still exists, and the key still opens it")
}
