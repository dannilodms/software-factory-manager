// Package provider names what the tool asks of the company that rents it
// its servers: create a server from a specification, find one by its name
// and destroy one by its name. Hetzner is the one adapter today, in the
// package hetzner; providertest keeps servers in memory for the tests of the
// commands.
package provider

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
)

// ErrNameTaken is the answer of Create when the provider already has a
// server, or a key, with the name of the new one: another turn-on may be
// creating it at this moment, so nothing of it is destroyed.
var ErrNameTaken = errors.New("the provider already has a server or a key with this name")

// Provider creates, finds and destroys the servers of one environment.
type Provider interface {
	// Create creates a server and returns once the provider says it is
	// running. On a failure it leaves nothing of the server behind that it
	// knows of; Destroy removes the rest.
	Create(ctx context.Context, specification Specification) (Server, error)
	// Find tells whether a server with that name exists, and gives it.
	Find(ctx context.Context, name string) (Server, bool, error)
	// Destroy destroys the server with that name and the deploy key
	// registered with it. Destroying what does not exist succeeds.
	Destroy(ctx context.Context, name string) error
}

// Specification is what a new server is made of.
type Specification struct {
	Name        string
	Size        string
	Region      string
	SystemImage string
	// CloudInit is the user data of the server, the cloud-init file with the
	// keys of this server. It holds the private host key, so it prints as a
	// placeholder.
	CloudInit config.Secret
	// DeployPublicKey is the public key of the deploy user, as a line of
	// authorized keys.
	DeployPublicKey string
	// Labels mark what the provider creates for the environment.
	Labels map[string]string
}

// Server is a server that exists at the provider.
type Server struct {
	Name string
	// Address is its public IPv4 address.
	Address netip.Addr
	// Created is when the provider created it.
	Created time.Time
}
