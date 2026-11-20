package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
)

// What the GitHub Environment of an environment holds while it is on: what
// reaches its server, which the publish of the pipeline reads, and the time
// it was turned on, which the reminder reads. Turning it off deletes all but
// the password of Mailpit (Secrets in Environments in CODING_STANDARDS.md).
const (
	// deployKeySecret is the private deploy key, in OpenSSH's format.
	deployKeySecret = "DEPLOY_KEY"
	// mailpitPasswordSecret is the password the browser asks for on the host
	// name of Mailpit.
	mailpitPasswordSecret = "MAILPIT_PASSWORD"
	// addressVariable is the IPv4 address of the server. A publish takes the
	// environment for off when it is empty.
	addressVariable = "SERVER_ADDRESS"
	// hostKeyVariable is the host key of the server, as a line of known
	// hosts.
	hostKeyVariable = "SERVER_HOST_KEY"
	// turnedOnAtVariable is when the server was created at the provider, in
	// RFC 3339 and UTC.
	turnedOnAtVariable = "TURNED_ON_AT"
)

// labelColor is the color of the label that publishes a pull request.
const labelColor = "FBCA04"

// serverValues are what GitHub gets of a server that is on: where it is,
// the keys that log in to it, and when it was created.
type serverValues struct {
	address  netip.Addr
	keys     loginKeys
	turnedOn time.Time
}

// label is the label that publishes a pull request to the environment.
func (e environment) label() github.Label {
	return github.Label{
		Name:        e.name,
		Description: "Publish this pull request to " + e.name + " once its checks pass",
		Color:       labelColor,
	}
}

// prepareGitHub makes sure the GitHub Environment and the label exist, so
// that a turn-on that cannot reach GitHub stops before it costs anything.
func (e environment) prepareGitHub(ctx context.Context) error {
	if err := e.outside.github.EnsureEnvironment(ctx, e.name); err != nil {
		return fmt.Errorf("make sure the GitHub Environment %s exists: %w", e.name, err)
	}
	if err := e.outside.github.EnsureLabel(ctx, e.label()); err != nil {
		return fmt.Errorf("make sure the label %s exists on GitHub: %w", e.name, err)
	}
	return nil
}

// writeValues gives the GitHub Environment what reaches the server, and the
// address last: a publish takes the environment for on once it has one.
func (e environment) writeValues(ctx context.Context, server serverValues, mailpitPassword config.Secret, stdout io.Writer) error {
	deployKey := server.keys.deployPrivate
	address := server.address.String()
	hostKey := knownhosts.Line([]string{address}, server.keys.host)
	turnedOn := server.turnedOn.UTC().Format(time.RFC3339)
	err := inOrder(
		func() error { return e.outside.github.SetSecret(ctx, e.name, deployKeySecret, deployKey) },
		func() error { return e.outside.github.SetSecret(ctx, e.name, mailpitPasswordSecret, mailpitPassword) },
		func() error { return e.outside.github.SetVariable(ctx, e.name, hostKeyVariable, hostKey) },
		func() error { return e.outside.github.SetVariable(ctx, e.name, turnedOnAtVariable, turnedOn) },
		func() error { return e.outside.github.SetVariable(ctx, e.name, addressVariable, address) },
	)
	if err != nil {
		return fmt.Errorf("give the GitHub Environment %s the values of the server: %w", e.name, err)
	}
	fmt.Fprintf(stdout, "The GitHub Environment %s has the address, the host key and the deploy key of the server, and the password of Mailpit.\n", e.name)
	return nil
}

// deleteValues takes out of the GitHub Environment what reached the server,
// the address first, and keeps the password of Mailpit.
func (e environment) deleteValues(ctx context.Context) error {
	err := inOrder(
		func() error { return e.outside.github.DeleteVariable(ctx, e.name, addressVariable) },
		func() error { return e.outside.github.DeleteVariable(ctx, e.name, hostKeyVariable) },
		func() error { return e.outside.github.DeleteVariable(ctx, e.name, turnedOnAtVariable) },
		func() error { return e.outside.github.DeleteSecret(ctx, e.name, deployKeySecret) },
	)
	if err != nil {
		return fmt.Errorf("delete the values of the server from the GitHub Environment %s: %w", e.name, err)
	}
	return nil
}

// mailpitPassword gives the password of the environment's Mailpit: the one
// the .env of the main checkout keeps, or else a new one, which it appends
// there. GitHub never gives a secret back, so that file is where the
// maintainer reads it.
func (e environment) mailpitPassword(stdout io.Writer) (config.Secret, error) {
	name := mailpitPasswordName(e.name)
	kept, err := e.secret(name)
	if err != nil {
		return config.Secret{}, err
	}
	if kept.Reveal() != "" {
		return kept, nil
	}
	chosen := rand.Text()
	comment := fmt.Sprintf("# The password of the Mailpit of %s, chosen when %s was first turned on.\n", e.name, e.name)
	if err := appendTo(e.checkout.envFile(), comment+name+"="+chosen+"\n"); err != nil {
		return config.Secret{}, fmt.Errorf("keep the password of Mailpit in %s: %w", e.checkout.envFile(), err)
	}
	fmt.Fprintf(stdout, "Chose a password for the Mailpit of %s and kept it in %s, as %s.\n", e.name, e.checkout.envFile(), name)
	return config.NewSecret(chosen), nil
}

// mailpitPasswordName is the variable of the .env of the main checkout that
// keeps the password of the environment's Mailpit. It is not in
// .env.example: no server and no publish reads it.
func mailpitPasswordName(environment string) string {
	return strings.ToUpper(strings.ReplaceAll(environment, "-", "_")) + "_MAILPIT_PASSWORD"
}
