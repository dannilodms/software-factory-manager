package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aWorktree makes the directory of the world the main checkout of a
// repository, writes a worktree of it as Git does, under .claude/worktrees,
// and runs the commands in the worktree, with no token among the variables
// of the process. It gives the main checkout.
func aWorktree(t *testing.T, world *world, env string) string {
	t.Helper()
	main := world.directory
	worktree := filepath.Join(main, ".claude", "worktrees", "feature")
	copyCommitted(t, worktree)
	gitDirectory := filepath.Join(main, ".git", "worktrees", "feature")
	require.NoError(t, os.MkdirAll(gitDirectory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(gitDirectory, "commondir"), []byte("../..\n"), 0o600))
	pointer := "gitdir: " + filepath.ToSlash(gitDirectory) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".git"), []byte(pointer), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(main, ".env"), []byte(env), 0o600))
	world.directory = filepath.Join(worktree, "apps", "api")
	require.NoError(t, os.MkdirAll(world.directory, 0o700))
	world.variables = map[string]string{"POSTGRES_PORT": "5432"}
	return main
}

func TestInAWorktreeTheTokenIsReadFromTheEnvOfTheMainCheckout(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	main := aWorktree(t, world, `# The token of the project at Hetzner
POSTGRES_PORT=5433
HETZNER_TOKEN="`+token+`" # staging only
`)

	world.succeeds("on", "staging")

	require.Len(t, world.tokens, 1)
	assert.Equal(t, token, world.tokens[0].Reveal())
	assert.FileExists(t, filepath.Join(main, ".servers", "staging", "deploy-key"), "the key is kept in the main checkout, which outlives the worktree")
	assert.NoDirExists(t, filepath.Join(main, ".claude", "worktrees", "feature", ".servers"))
}

func TestTheValueOfTheTokenInTheEnvOfTheMainCheckoutIsReadAsTaskReadsIt(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"unquoted":              "HETZNER_TOKEN=" + token + "\n",
		"unquoted, commented":   "HETZNER_TOKEN=" + token + " # staging\n",
		"single quotes":         "HETZNER_TOKEN='" + token + "'\n",
		"exported":              "export HETZNER_TOKEN=" + token + "\n",
		"spaces around":         "  HETZNER_TOKEN = " + token + "  \n",
		"line ends with CR LF":  "HETZNER_TOKEN=" + token + "\r\n",
		"the last line wins":    "HETZNER_TOKEN=an-old-token\nHETZNER_TOKEN=" + token + "\n",
		"a comment is not read": "# HETZNER_TOKEN=an-old-token\nHETZNER_TOKEN=" + token + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)
			aWorktree(t, world, line)

			world.succeeds("status", "staging")

			require.Len(t, world.tokens, 1)
			assert.Equal(t, token, world.tokens[0].Reveal())
		})
	}
}

func TestAVariableOfTheProcessWinsOverTheEnvOfTheMainCheckout(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	aWorktree(t, world, "HETZNER_TOKEN=a-token-of-the-file\n")
	world.variables["HETZNER_TOKEN"] = token

	world.succeeds("status", "staging")

	require.Len(t, world.tokens, 1)
	assert.Equal(t, token, world.tokens[0].Reveal())
}

func TestWithNoTokenTheCommandsStopBeforeReachingTheProvider(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"on", "off", "status"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)
			main := aWorktree(t, world, "POSTGRES_PORT=5433\nHETZNER_TOKEN=\n")

			ran := world.run(command, "staging")

			assert.Equal(t, 1, ran.status)
			assert.Equal(t, "environment: HETZNER_TOKEN is required to reach hetzner: write it in the .env of the main checkout, "+filepath.Join(main, ".env")+"\n", ran.stderr)
			assert.Empty(t, ran.stdout)
			assert.Empty(t, world.tokens, "the provider was not reached")
		})
	}
}

func TestOutsideACheckoutTheCommandsStop(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.directory = t.TempDir()

	ran := world.run("status", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Contains(t, ran.stderr, "is not inside a checkout of the repository\n")
}
