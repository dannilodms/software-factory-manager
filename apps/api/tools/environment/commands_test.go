package main

import (
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoCommandPrintsASecret(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	var printed strings.Builder
	record := func(ran result) {
		printed.WriteString(ran.stdout)
		printed.WriteString(ran.stderr)
	}

	record(world.run("on", "staging"))
	record(world.run("on", "staging"))
	record(world.run("status", "staging"))
	deployKey, err := os.ReadFile(filepath.Join(world.localCopy(), "deploy-key"))
	require.NoError(t, err)
	hostKey := cloudConfigOf(t, world.specification()).SSHKeys.Private
	record(world.run("off", "staging"))
	world.readiness.err = errors.New("not ready")
	record(world.run("on", "staging"))
	world.readiness.err = nil
	world.github.FailSettingWith("SERVER_ADDRESS", errors.New("HTTP 502"))
	record(world.run("on", "staging"))

	secrets := map[string]string{"the token": token, "the password of Mailpit": world.mailpitPassword(world.directory)}
	for name, key := range map[string]string{"the deploy key": string(deployKey), "the host key": hostKey} {
		block, _ := pem.Decode([]byte(key))
		require.NotNil(t, block)
		secrets[name] = key
		secrets[name+", in base64"] = strings.Split(key, "\n")[1]
	}
	for name, secret := range secrets {
		assert.NotContains(t, printed.String(), secret, "%s is printed", name)
	}
}

func TestACommandThatIsNotUnderstoodExitsWithTwo(t *testing.T) {
	t.Parallel()
	for name, arguments := range map[string][]string{
		"no command":                     {},
		"no environment":                 {"on"},
		"an unknown command":             {"start", "staging"},
		"too many arguments":             {"on", "staging", "now"},
		"an environment with no choices": {"on", "production"},
		"a name that is a path":          {"on", "../staging"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)

			ran := world.run(arguments...)

			assert.Equal(t, 2, ran.status)
			assert.Contains(t, ran.stderr, "Usage: environment <command> <environment>")
			assert.Empty(t, world.tokens, "the provider was not reached")
		})
	}
}

func TestAnUnknownEnvironmentNamesTheFileItsChoicesWouldBeIn(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	ran := world.run("on", "production")

	assert.True(t, strings.HasPrefix(ran.stderr, "environment: unknown environment \"production\": deploy/server.production.yaml does not exist\n"), ran.stderr)
}

func TestChoicesThatCannotBeUsedStopTheCommands(t *testing.T) {
	t.Parallel()
	valid := "provider: hetzner\nregion: fsn1\nsize: cx23\nsystem_image: ubuntu-26.04\nserver_name: staging\n"
	for name, test := range map[string]struct{ choices, failure string }{
		"a choice missing": {
			choices: strings.Replace(valid, "region: fsn1\n", "", 1),
			failure: "environment: deploy/server.staging.yaml: region is missing\n",
		},
		"a provider with no adapter": {
			choices: strings.Replace(valid, "hetzner", "aws", 1),
			failure: "environment: deploy/server.staging.yaml: the tool has no adapter for the provider \"aws\", only for hetzner\n",
		},
		"an unknown choice": {
			choices: valid + "datacenter: fsn1-dc14\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)
			require.NoError(t, os.WriteFile(filepath.Join(world.directory, "deploy", "server.staging.yaml"), []byte(test.choices), 0o600))

			ran := world.run("status", "staging")

			assert.Equal(t, 1, ran.status)
			assert.Empty(t, world.tokens, "the provider was not reached")
			if test.failure == "" {
				assert.Contains(t, ran.stderr, "environment: read deploy/server.staging.yaml: ")
				return
			}
			assert.Equal(t, test.failure, ran.stderr)
		})
	}
}
