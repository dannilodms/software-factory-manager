// Package sshclient reaches a server over SSH as its deploy user, trusting
// only the host key the tool gave the server: a server that answers with
// another key is refused before the deploy key is offered. Readiness waits
// until a new server answers and cloud-init has prepared it, and Remote is
// the server that a publish writes files to and runs commands on.
package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// dialTimeout bounds one attempt to connect, when the context allows more.
const dialTimeout = 15 * time.Second

// ErrOtherHostKey is the refusal of a server whose host key is not the one
// expected.
var ErrOtherHostKey = errors.New("the server answered with another host key")

// Target is a server and how to log in to it.
type Target struct {
	// Address is where its SSH listens, as host:port.
	Address string
	// HostKey is the only host key trusted.
	HostKey ssh.PublicKey
	// User logs in with Key.
	User string
	Key  ssh.Signer
}

// Dial connects and logs in to the target. The context bounds the
// connection and the handshake; the client it gives lives until it is
// closed.
func Dial(ctx context.Context, target Target) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:              target.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(target.Key)},
		HostKeyCallback:   hostKeyIs(target.HostKey),
		HostKeyAlgorithms: []string{target.HostKey.Type()},
		Timeout:           dialTimeout,
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", target.Address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	client, channels, requests, err := ssh.NewClientConn(connection, target.Address, config)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(client, channels, requests), nil
}

// hostKeyIs trusts the expected key and no other.
func hostKeyIs(expected ssh.PublicKey) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if bytes.Equal(key.Marshal(), expected.Marshal()) {
			return nil
		}
		return ErrOtherHostKey
	}
}

// Run runs a command on the server and gives the status it exited with.
// The command carries no value: values travel as the content of a file.
func Run(ctx context.Context, client *ssh.Client, command string) (int, error) {
	return runWith(ctx, client, command, streams{})
}

// streams are what a command reads and where what it prints goes. A nil
// one reads nothing, or discards what is printed.
type streams struct {
	input               io.Reader
	output, errorOutput io.Writer
}

// runWith runs a command with those streams. The end of the context closes
// the connection, which ends the command.
func runWith(ctx context.Context, client *ssh.Client, command string, with streams) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		return 0, err
	}
	defer session.Close()
	session.Stdin, session.Stdout, session.Stderr = with.input, with.output, with.errorOutput
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	err = session.Run(command)
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return exit.ExitStatus(), nil
	}
	if err != nil {
		return 0, fmt.Errorf("run %q: %w", command, err)
	}
	return 0, nil
}
