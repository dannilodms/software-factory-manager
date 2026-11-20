package sshclient_test

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient/sshclienttest"
)

// These tests wait for an SSH server that the test starts in its own
// process, as the server of an environment would answer once cloud-init
// has prepared it.

const (
	cloudInitWait = "cloud-init status --wait"
	preparedCheck = "test -f /var/lib/software-factory-manager/server-prepared"
)

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	return signer
}

// aServer starts a server with its own host key, which lets in the deploy
// user with the deploy key, and gives the target that reaches it.
func aServer(t *testing.T) (*sshclienttest.Server, sshclient.Target) {
	t.Helper()
	hostKey, deployKey := newSigner(t), newSigner(t)
	server := sshclienttest.NewServer(t, hostKey, "deploy", deployKey.PublicKey())
	return server, sshclient.Target{Address: server.Address, HostKey: hostKey.PublicKey(), User: "deploy", Key: deployKey}
}

func lines(commands []sshclienttest.Command) []string {
	result := make([]string, 0, len(commands))
	for _, command := range commands {
		result = append(result, command.Line)
	}
	return result
}

func quickly() sshclient.Readiness {
	return sshclient.Readiness{RetryInterval: 10 * time.Millisecond}
}

func TestAServerIsReadyOnceCloudInitHasFinishedAndTheServerIsPrepared(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)

	err := quickly().Wait(context.Background(), target)

	require.NoError(t, err)
	assert.Equal(t, []string{cloudInitWait, preparedCheck}, lines(server.Commands()))
}

func TestAServerIsReadyWhenCloudInitFinishedWithAWarningAndTheServerIsPrepared(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	server.Answer(cloudInitWait, 2)

	err := quickly().Wait(context.Background(), target)

	require.NoError(t, err)
}

func TestTheWaitTriesAgainUntilTheServerLetsTheDeployUserIn(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	server.RefuseLogins(3)

	err := quickly().Wait(context.Background(), target)

	require.NoError(t, err)
	assert.Equal(t, []string{cloudInitWait, preparedCheck}, lines(server.Commands()))
}

func TestAServerWithAnotherHostKeyIsRefusedBeforeTheDeployKeyIsOffered(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	target.HostKey = newSigner(t).PublicKey()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := quickly().Wait(ctx, target)

	require.ErrorIs(t, err, sshclient.ErrOtherHostKey)
	assert.Contains(t, err.Error(), "was not ready in time")
	assert.Zero(t, server.Offered(), "the deploy key is never offered to a server that answers with another host key")
	assert.Empty(t, server.Commands())
}

func TestAFailedCloudInitEndsTheWaitAtOnce(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	server.Answer(cloudInitWait, 1)

	err := quickly().Wait(context.Background(), target)

	require.EqualError(t, err, "cloud-init failed on the server, with status 1: `cloud-init status --long` there tells why")
	assert.Equal(t, []string{cloudInitWait}, lines(server.Commands()))
}

func TestACloudInitFileThatDidNotRunToItsEndEndsTheWaitAtOnce(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	server.Answer(preparedCheck, 1)

	err := quickly().Wait(context.Background(), target)

	require.ErrorContains(t, err, "the cloud-init file did not run to its end")
}

func TestAServerThatNeverAnswersIsGivenUpAtTheEndOfTheContext(t *testing.T) {
	t.Parallel()
	_, target := aServer(t)
	target.Address = "127.0.0.1:1"
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := quickly().Wait(ctx, target)

	require.ErrorContains(t, err, "the server at 127.0.0.1:1 was not ready in time")
}
