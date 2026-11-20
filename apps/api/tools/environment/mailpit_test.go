package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheFirstTurnOnChoosesThePasswordOfMailpitAndKeepsItInTheEnvOfTheMainCheckout(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	env, err := os.ReadFile(filepath.Join(world.directory, ".env"))
	require.NoError(t, err)
	password := world.mailpitPassword(world.directory)
	assert.Len(t, password, 26, "128 bits of crypto/rand.Text")
	assert.Equal(t, "# The password of the Mailpit of staging, chosen when staging was first turned on.\nSTAGING_MAILPIT_PASSWORD="+password+"\n", string(env))
	assert.Equal(t, password, world.github.Secrets("staging")["MAILPIT_PASSWORD"])
}

func TestTheEnvTheFirstTurnOnCreatesIsReachedByItsOwnerAlone(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	assertOwnerAlone(t, filepath.Join(world.directory, ".env"))
}

func TestTheNextTurnOnReusesThePasswordOfMailpit(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	password := world.mailpitPassword(world.directory)
	world.succeeds("off", "staging")
	world.github.Clear("staging")

	printed := world.succeeds("on", "staging")

	assert.Equal(t, password, world.mailpitPassword(world.directory), "the .env keeps one password")
	assert.Equal(t, password, world.github.Secrets("staging")["MAILPIT_PASSWORD"])
	assert.NotContains(t, printed, "Chose a password")
}

func TestInAWorktreeThePasswordOfMailpitIsKeptInTheEnvOfTheMainCheckoutWithEveryLineBeforeIt(t *testing.T) {
	t.Parallel()
	for name, before := range map[string]string{
		"lines that end with LF":       "# The token\nHETZNER_TOKEN=" + token + "\n",
		"lines that end with CR LF":    "# The token\r\nHETZNER_TOKEN=" + token + "\r\n",
		"a last line with no line end": "# The token\nHETZNER_TOKEN=" + token,
		"CR LF, and no line end last":  "# The token\r\nHETZNER_TOKEN=" + token,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)
			main := aWorktree(t, world, before)

			world.succeeds("on", "staging")

			env, err := os.ReadFile(filepath.Join(main, ".env"))
			require.NoError(t, err)
			password := world.mailpitPassword(main)
			newline := "\n"
			if strings.Contains(before, "\r\n") {
				newline = "\r\n"
			}
			after := strings.TrimSuffix(before, newline) + newline +
				"# The password of the Mailpit of staging, chosen when staging was first turned on." + newline +
				"STAGING_MAILPIT_PASSWORD=" + password + newline
			assert.Equal(t, after, string(env))
			assert.NoFileExists(t, filepath.Join(main, ".claude", "worktrees", "feature", ".env"))
			assert.Equal(t, password, world.github.Secrets("staging")["MAILPIT_PASSWORD"])
		})
	}
}

func TestAPasswordOfMailpitAmongTheVariablesOfTheProcessIsReusedAndNotWritten(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.variables["STAGING_MAILPIT_PASSWORD"] = "PASSWORDTASKREADFROMTHEENV"

	world.succeeds("on", "staging")

	assert.NoFileExists(t, filepath.Join(world.directory, ".env"))
	assert.Equal(t, "PASSWORDTASKREADFROMTHEENV", world.github.Secrets("staging")["MAILPIT_PASSWORD"])
}

func TestThePasswordOfMailpitIsNotAVariableOfEnvExample(t *testing.T) {
	t.Parallel()
	example, err := os.ReadFile(filepath.Join(repository, ".env.example"))
	require.NoError(t, err)

	assert.NotContains(t, string(example), "STAGING_MAILPIT_PASSWORD", "the publish asks a staging value for every name of .env.example")
	assert.NotContains(t, string(example), "HETZNER_TOKEN")
}

func TestAPasswordOfMailpitThatCannotBeKeptStopsTheTurnOnBeforeTheProvider(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	require.NoError(t, os.Mkdir(filepath.Join(world.directory, ".env"), 0o700))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Contains(t, ran.stderr, "environment: read STAGING_MAILPIT_PASSWORD from "+filepath.Join(world.directory, ".env")+": ")
	assert.Empty(t, world.provider.Servers())
	assert.Empty(t, world.github.Environments())
}
