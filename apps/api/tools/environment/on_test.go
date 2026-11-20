package main

import (
	"crypto/ed25519"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
	"golang.org/x/crypto/ssh"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
)

// cloudConfig is the part of a cloud-init file the tests read.
type cloudConfig struct {
	SSHKeys struct {
		Private string `yaml:"ed25519_private"`
		Public  string `yaml:"ed25519_public"`
	} `yaml:"ssh_keys"`
	Users []struct {
		Name           string   `yaml:"name"`
		AuthorizedKeys []string `yaml:"ssh_authorized_keys"`
	} `yaml:"users"`
}

func cloudConfigOf(t *testing.T, specification provider.Specification) cloudConfig {
	t.Helper()
	content := specification.CloudInit.Reveal()
	require.Regexp(t, `^#cloud-config\n`, content)
	var file cloudConfig
	require.NoError(t, yaml.Unmarshal([]byte(content), &file))
	return file
}

func TestTurningStagingOnCreatesOneServerFromItsCommittedChoices(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	printed := world.succeeds("on", "staging")

	require.Equal(t, []provider.Server{{
		Name: "staging", Address: netip.MustParseAddr("203.0.113.10"), Created: world.clock.now,
	}}, world.provider.Servers())
	specification := world.specification()
	assert.Equal(t, "cx23", specification.Size)
	assert.Equal(t, "fsn1", specification.Region)
	assert.Equal(t, "ubuntu-26.04", specification.SystemImage, "the newest Ubuntu LTS that Docker supports officially")
	assert.Equal(t, map[string]string{"environment": "staging"}, specification.Labels)
	assert.Equal(t, []map[string]string{{"environment": "staging"}}, world.labels, "the provider finds what it created by the labels")
	assert.Equal(t, `Chose a password for the Mailpit of staging and kept it in `+filepath.Join(world.directory, ".env")+`, as STAGING_MAILPIT_PASSWORD.
Turning staging on: creating the server staging at hetzner, a cx23 with ubuntu-26.04 in fsn1.
The server is at 203.0.113.10. Waiting until it answers SSH with its host key and cloud-init has prepared it, for up to 10m0s.
The GitHub Environment staging has the address, the host key and the deploy key of the server, and the password of Mailpit.
Staging is on.
  App:    https://app.203-0-113-10.sslip.io
  Files:  https://files.203-0-113-10.sslip.io
  Mail:   https://mail.203-0-113-10.sslip.io
  SSH:    ssh -F "`+filepath.ToSlash(filepath.Join(world.localCopy(), "ssh_config"))+`" staging
`, printed)
}

func TestTheServerIsPreparedByTheCloudInitFileWithTheKeysOfTheTurnOn(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	specification := world.specification()
	file := cloudConfigOf(t, specification)
	require.Len(t, world.readiness.targets, 1)
	target := world.readiness.targets[0]
	assert.Equal(t, "203.0.113.10:22", target.Address)
	assert.Equal(t, "deploy", target.User)

	hostKey, err := ssh.ParseRawPrivateKey([]byte(file.SSHKeys.Private))
	require.NoError(t, err, "the private host key is a key OpenSSH reads")
	hostPublic, err := ssh.NewPublicKey(hostKey.(*ed25519.PrivateKey).Public())
	require.NoError(t, err)
	assert.Equal(t, authorizedLine(hostPublic), file.SSHKeys.Public, "the public host key is the private one's")
	assert.Equal(t, authorizedLine(hostPublic), authorizedLine(target.HostKey), "the server is trusted with the host key it was given, and no other")

	require.Len(t, file.Users, 1)
	assert.Equal(t, "deploy", file.Users[0].Name)
	assert.Equal(t, []string{specification.DeployPublicKey}, file.Users[0].AuthorizedKeys)
	assert.Equal(t, specification.DeployPublicKey, authorizedLine(target.Key.PublicKey()), "the deploy key logs in as the deploy user")
}

func TestEachTurnOnDrawsNewKeys(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	first := world.specification()
	world.succeeds("off", "staging")

	world.succeeds("on", "staging")

	second := world.specification()
	assert.NotEqual(t, first.DeployPublicKey, second.DeployPublicKey)
	assert.NotEqual(t, cloudConfigOf(t, first).SSHKeys.Public, cloudConfigOf(t, second).SSHKeys.Public)
}

func TestTurningOnKeepsTheDeployKeyInAFolderOfTheMainCheckoutThatGitIgnores(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	world.succeeds("on", "staging")

	specification := world.specification()
	deployKey, err := os.ReadFile(filepath.Join(world.localCopy(), "deploy-key"))
	require.NoError(t, err)
	signer, err := ssh.ParsePrivateKey(deployKey)
	require.NoError(t, err)
	assert.Equal(t, specification.DeployPublicKey, authorizedLine(signer.PublicKey()))
	knownHosts, err := os.ReadFile(filepath.Join(world.localCopy(), "known_hosts"))
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.10 "+cloudConfigOf(t, specification).SSHKeys.Public+"\n", string(knownHosts))
	sshConfig, err := os.ReadFile(filepath.Join(world.localCopy(), "ssh_config"))
	require.NoError(t, err)
	assert.Contains(t, string(sshConfig), "\nHost staging\n  HostName 203.0.113.10\n  User deploy\n")

	ignored, err := os.ReadFile(filepath.Join(repository, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(ignored), "\n/.servers/\n", "git ignores the folder of the local copies")
}

func TestTurningOnWhenOnCreatesNothingAndSaysSo(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	first := world.specification()

	printed := world.succeeds("on", "staging")

	assert.Len(t, world.provider.Servers(), 1)
	assert.Equal(t, first, world.specification(), "the server is the one of the first turn-on")
	assert.Equal(t, `Staging is on already, at 203.0.113.10. Checking that it answers.
It answers: nothing was created.
The GitHub Environment staging has the address, the host key and the deploy key of the server, and the password of Mailpit.
  App:    https://app.203-0-113-10.sslip.io
  Files:  https://files.203-0-113-10.sslip.io
  Mail:   https://mail.203-0-113-10.sslip.io
  SSH:    ssh -F "`+filepath.ToSlash(filepath.Join(world.localCopy(), "ssh_config"))+`" staging
`, printed)
	require.Len(t, world.readiness.targets, 2)
	second := world.readiness.targets[1]
	assert.Equal(t, authorizedLine(world.readiness.targets[0].HostKey), authorizedLine(second.HostKey), "it is checked with the host key it was given")
	assert.Equal(t, first.DeployPublicKey, authorizedLine(second.Key.PublicKey()), "and with the deploy key the main checkout kept")
}

func TestTurningOnWhenOnButNotAnsweringFailsAndDestroysNothing(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.readiness.err = errors.New("the server at 203.0.113.10:22 was not ready in time: connection refused")

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: staging is on at 203.0.113.10, but does not answer: the server at 203.0.113.10:22 was not ready in time: connection refused\n", ran.stderr)
	assert.Len(t, world.provider.Servers(), 1)
	assert.DirExists(t, world.localCopy())
}

func TestTurningOnWhenOnWithoutALocalCopyOfTheKeyFailsAndDestroysNothing(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.provider.Add(provider.Server{Name: "staging", Address: netip.MustParseAddr("198.51.100.7"), Created: world.clock.now})

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: staging is on at 198.51.100.7, but "+world.localCopy()+" keeps no copy of its deploy key: turn staging off and on again\n", ran.stderr)
	assert.Len(t, world.provider.Servers(), 1)
	assert.Empty(t, world.readiness.targets)
}

func TestAServerThatIsNeverReadyIsDestroyedAndItsKeyIsNotKept(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readiness.err = errors.New("cloud-init failed on the server, with status 1")

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: wait until the server is ready: cloud-init failed on the server, with status 1; the server was destroyed, and nothing of it is left\n", ran.stderr)
	assert.Empty(t, world.provider.Servers())
	assert.NoDirExists(t, world.localCopy())
}

func TestACreationThatFailsAfterTheServerExistsDestroysIt(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.provider.CreateButFailWith(errors.New("wait until the server runs: action failed"))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: create the server staging: wait until the server runs: action failed; the server was destroyed, and nothing of it is left\n", ran.stderr)
	assert.Empty(t, world.provider.Servers())
	assert.Empty(t, world.readiness.targets)
}

func TestAFailedTurnOnThatCannotDestroyTheServerSaysToTurnStagingOff(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readiness.err = errors.New("not ready")
	world.provider.FailDestroyingWith(errors.New("the provider does not answer"))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: wait until the server is ready: not ready; undoing it failed too (destroy the server: the provider does not answer): turn staging off\n", ran.stderr)
	assert.NoDirExists(t, world.localCopy())
}

func TestATurnOnThatMeetsTheNameTakenDestroysNothing(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.provider.FailCreatingWith(provider.ErrNameTaken)
	world.provider.FailDestroyingWith(errors.New("destroying is not what a taken name asks for"))

	ran := world.run("on", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: create the server staging: the provider already has a server or a key with this name. Another turn-on may be creating it now; when none is, turn staging off and on again\n", ran.stderr)
}
