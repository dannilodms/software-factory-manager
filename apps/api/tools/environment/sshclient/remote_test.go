package sshclient_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
)

// These tests work on a server through an SSH server that the test starts
// in its own process, which records each command and what it read on its
// input, as the server of an environment would receive them from a publish.

func TestWritingAFileSendsItsContentOnTheInputOfACommandThatCarriesNoneOfIt(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	remote, err := sshclient.Open(context.Background(), target, &bytes.Buffer{})
	require.NoError(t, err)
	defer remote.Close()
	content := []byte("LOGIN_CODE_KEY='6c6f67696e2d636f6465'\n")

	err = remote.WriteFile(context.Background(), "staging/.env", content, 0o600)

	require.NoError(t, err)
	commands := server.Commands()
	require.Len(t, commands, 1)
	assert.Equal(t, "umask 077 && mkdir -p 'staging' && cat > 'staging/.env.new' && chmod 0600 'staging/.env.new' && mv -f 'staging/.env.new' 'staging/.env'",
		commands[0].Line, "the file is written whole under another name, given its mode, and only then put in place, in a folder only the deploy user reads")
	assert.Equal(t, content, commands[0].Input)
}

func TestACommandPrintsWhatItPrintedOnTheServer(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	server.Print("docker compose pull --quiet", "pulled\n", "a warning\n")
	var output bytes.Buffer
	remote, err := sshclient.Open(context.Background(), target, &output)
	require.NoError(t, err)
	defer remote.Close()

	err = remote.Run(context.Background(), "docker compose pull --quiet")

	require.NoError(t, err)
	assert.Equal(t, []string{"docker compose pull --quiet"}, lines(server.Commands()))
	assert.Contains(t, output.String(), "pulled\n")
	assert.Contains(t, output.String(), "a warning\n")
}

func TestACommandThatExitsWithAnotherStatusThanZeroFails(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	server.Answer("docker compose pull --quiet", 18)
	remote, err := sshclient.Open(context.Background(), target, &bytes.Buffer{})
	require.NoError(t, err)
	defer remote.Close()

	err = remote.Run(context.Background(), "docker compose pull --quiet")

	require.EqualError(t, err, `"docker compose pull --quiet" exited with status 18`)
}

func TestAServerWithAnotherHostKeyIsNotWorkedOn(t *testing.T) {
	t.Parallel()
	server, target := aServer(t)
	target.HostKey = newSigner(t).PublicKey()

	_, err := sshclient.Open(context.Background(), target, &bytes.Buffer{})

	require.ErrorIs(t, err, sshclient.ErrOtherHostKey)
	assert.Zero(t, server.Offered(), "the deploy key is never offered to a server that answers with another host key")
	assert.Empty(t, server.Commands())
}
