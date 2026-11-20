// Package providertest keeps the servers of a provider in memory, for the
// tests of the commands of the tool.
package providertest

import (
	"cmp"
	"context"
	"net/netip"
	"slices"
	"time"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
)

// Provider is a provider whose servers live in memory. Each server gets the
// next address of a documentation range, 203.0.113.0/24.
type Provider struct {
	now            func() time.Time
	servers        map[string]provider.Server
	specifications map[string]provider.Specification
	next           netip.Addr
	createErr      error
	createdErr     error
	destroyErr     error
}

// NewProvider gives an empty provider, which dates what it creates with now.
func NewProvider(now func() time.Time) *Provider {
	return &Provider{
		now:            now,
		servers:        map[string]provider.Server{},
		specifications: map[string]provider.Specification{},
		next:           netip.MustParseAddr("203.0.113.10"),
	}
}

// FailCreatingWith makes every later Create fail with err and create
// nothing.
func (p *Provider) FailCreatingWith(err error) {
	p.createErr = err
}

// CreateButFailWith makes every later Create create the server and still
// fail with err, as when the provider gives up waiting for it.
func (p *Provider) CreateButFailWith(err error) {
	p.createdErr = err
}

// FailDestroyingWith makes every later Destroy fail with err and destroy
// nothing.
func (p *Provider) FailDestroyingWith(err error) {
	p.destroyErr = err
}

// Create creates a server, or answers provider.ErrNameTaken when one has
// the name.
func (p *Provider) Create(_ context.Context, specification provider.Specification) (provider.Server, error) {
	if p.createErr != nil {
		return provider.Server{}, p.createErr
	}
	if _, taken := p.servers[specification.Name]; taken {
		return provider.Server{}, provider.ErrNameTaken
	}
	server := provider.Server{Name: specification.Name, Address: p.next, Created: p.now()}
	p.next = p.next.Next()
	p.servers[server.Name] = server
	p.specifications[server.Name] = specification
	if p.createdErr != nil {
		return provider.Server{}, p.createdErr
	}
	return server, nil
}

// Find gives the server with that name.
func (p *Provider) Find(_ context.Context, name string) (provider.Server, bool, error) {
	server, found := p.servers[name]
	return server, found, nil
}

// Destroy destroys the server with that name, if there is one.
func (p *Provider) Destroy(_ context.Context, name string) error {
	if p.destroyErr != nil {
		return p.destroyErr
	}
	delete(p.servers, name)
	delete(p.specifications, name)
	return nil
}

// Servers gives the servers that exist, by name.
func (p *Provider) Servers() []provider.Server {
	servers := make([]provider.Server, 0, len(p.servers))
	for _, server := range p.servers {
		servers = append(servers, server)
	}
	slices.SortFunc(servers, func(a, b provider.Server) int { return cmp.Compare(a.Name, b.Name) })
	return servers
}

// SpecificationOf gives what the server with that name was created from.
func (p *Provider) SpecificationOf(name string) (provider.Specification, bool) {
	specification, found := p.specifications[name]
	return specification, found
}

// Add puts a server at the provider, as if another machine had created it.
func (p *Provider) Add(server provider.Server) {
	p.servers[server.Name] = server
}
