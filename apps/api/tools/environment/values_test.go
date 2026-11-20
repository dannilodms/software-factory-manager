package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider/providertest"
)

// mailpitPasswordLine is the line of the .env of the main checkout that
// keeps the password of the Mailpit of staging.
var mailpitPasswordLine = regexp.MustCompile(`(?m)^STAGING_MAILPIT_PASSWORD=([A-Z2-7]+)\r?$`)

// mailpitPassword is the password of Mailpit that the .env of the main
// checkout keeps.
func (w *world) mailpitPassword(main string) string {
	w.t.Helper()
	env, err := os.ReadFile(filepath.Join(main, ".env"))
	require.NoError(w.t, err)
	found := mailpitPasswordLine.FindAllStringSubmatch(string(env), -1)
	require.Len(w.t, found, 1, "the .env of the main checkout keeps one password of Mailpit")
	return found[0][1]
}

func TestTurningStagingOnGivesGitHubWhatReachesTheServer(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	assert.Equal(t, []string{"staging"}, world.github.Environments())
	deployKey, err := os.ReadFile(filepath.Join(world.localCopy(), "deploy-key"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"DEPLOY_KEY":       string(deployKey),
		"MAILPIT_PASSWORD": world.mailpitPassword(world.directory),
	}, world.github.Secrets("staging"))
	assert.Equal(t, map[string]string{
		"SERVER_ADDRESS":  "203.0.113.10",
		"SERVER_HOST_KEY": "203.0.113.10 " + cloudConfigOf(t, world.specification()).SSHKeys.Public,
		"TURNED_ON_AT":    "2026-11-19T14:00:00Z",
	}, world.github.Variables("staging"))
}

func TestTurningOnMakesSureTheEnvironmentAndTheLabelExist(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	assert.Equal(t, []string{"staging"}, world.github.Environments())
	assert.Equal(t, map[string]github.Label{"staging": {
		Name: "staging", Description: "Publish this pull request to staging once its checks pass", Color: "FBCA04",
	}}, world.github.Labels())
}

func TestTurningOnWhenOnGivesGitHubTheSameValuesAgain(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	first := world.github.Secrets("staging")
	firstVariables := world.github.Variables("staging")
	world.github.Clear("staging")
	world.clock.now = world.clock.now.Add(3 * time.Hour)

	world.succeeds("on", "staging")

	assert.Equal(t, first, world.github.Secrets("staging"))
	assert.Equal(t, firstVariables, world.github.Variables("staging"), "the time is when the server was created, not when it was turned on again")
}

func TestTurningOffTakesEverythingButThePasswordOfMailpitOutOfGitHub(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")

	world.succeeds("off", "staging")

	assert.Equal(t, map[string]string{"MAILPIT_PASSWORD": world.mailpitPassword(world.directory)}, world.github.Secrets("staging"))
	assert.Empty(t, world.github.Variables("staging"))
}

func TestTurningOffWhenOffTakesWhatIsLeftOutOfGitHub(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	// The server was destroyed at the provider by hand.
	world.provider = providertest.NewProvider(world.clock.Now)

	printed := world.succeeds("off", "staging")

	assert.Equal(t, "Staging was off already: there was no server to destroy.\nThe GitHub Environment staging keeps only the password of Mailpit.\n", printed)
	assert.Equal(t, []string{"MAILPIT_PASSWORD"}, slices.Sorted(maps.Keys(world.github.Secrets("staging"))))
	assert.Empty(t, world.github.Variables("staging"))
}

func TestTurningOffWhenGitHubHasNoEnvironmentSucceeds(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("off", "staging")

	assert.Empty(t, world.github.Environments(), "turning off creates nothing at GitHub")
}

func TestATurnOffThatCannotReachGitHubSaysToTurnStagingOffAgain(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.github.Unreachable(errors.New("gh is not logged in"))

	ran := world.run("off", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Empty(t, world.provider.Servers(), "the server, which costs, is destroyed first")
	assert.NoDirExists(t, world.localCopy())
	assert.Equal(t, "environment: the server staging was destroyed, but its values are still in GitHub (delete the values of the server from the GitHub Environment staging: gh is not logged in): turn staging off again\n", ran.stderr)
}

func TestATurnOnThatFailsAfterWritingToGitHubTakesTheValuesOutAgain(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.FailSettingWith("SERVER_ADDRESS", errors.New("HTTP 502"))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: give the GitHub Environment staging the values of the server: HTTP 502; the server was destroyed, and nothing of it is left\n", ran.stderr)
	assert.Empty(t, world.provider.Servers())
	assert.NoDirExists(t, world.localCopy())
	assert.Equal(t, map[string]string{"MAILPIT_PASSWORD": world.mailpitPassword(world.directory)}, world.github.Secrets("staging"))
	assert.Empty(t, world.github.Variables("staging"))
}

func TestATurnOnThatFailsBeforeWritingLeavesGitHubWithThePasswordOfMailpitAlone(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.succeeds("off", "staging")
	world.readiness.err = errors.New("not ready")

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, []string{"MAILPIT_PASSWORD"}, slices.Sorted(maps.Keys(world.github.Secrets("staging"))))
	assert.Empty(t, world.github.Variables("staging"))
}

func TestATurnOnThatCannotReachGitHubCreatesNoServer(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.Unreachable(errors.New("gh is not logged in"))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: make sure the GitHub Environment staging exists: gh is not logged in\n", ran.stderr)
	assert.Empty(t, world.provider.Servers())
	assert.Empty(t, world.readiness.targets)
}

func TestTurningOnWhenOnButGitHubFailsDestroysNothing(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.github.FailSettingWith("DEPLOY_KEY", errors.New("HTTP 502"))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: staging is on at 203.0.113.10, but GitHub does not have its values (give the GitHub Environment staging the values of the server: HTTP 502): turn staging on again\n", ran.stderr)
	assert.Len(t, world.provider.Servers(), 1)
	assert.DirExists(t, world.localCopy())
}

func TestTheTokenOfTheProviderNeverReachesGitHub(t *testing.T) {
	t.Parallel()
	for name, inAWorktree := range map[string]bool{
		"from the variables of the process":  false,
		"from the .env of the main checkout": true,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)
			if inAWorktree {
				aWorktree(t, world, "HETZNER_TOKEN="+token+"\n")
			}

			world.succeeds("on", "staging")
			world.succeeds("on", "staging")
			world.succeeds("status", "staging")
			world.succeeds("off", "staging")
			world.github.FailSettingWith("SERVER_ADDRESS", errors.New("HTTP 502"))
			world.run("on", "staging")

			written := world.github.Written()
			require.NotEmpty(t, written)
			for _, value := range written {
				assert.NotContains(t, value, token)
			}
		})
	}
}
