// Package hetzner is the adapter of Hetzner Cloud, on hcloud-go. Each server
// gets the deploy public key registered as a key of the project, under the
// server's name, so that Hetzner sends no root password, and an IPv4 address
// of its own; destroying the server removes both. Everything it creates
// carries the labels of the environment, and it finds things by name and by
// those labels. It creates no firewall at Hetzner: the firewall is inside
// the server, set by the cloud-init file.
package hetzner

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
)

// Endpoint is the address of Hetzner Cloud's API.
const Endpoint = hcloud.Endpoint

// Settings are what the adapter needs.
type Settings struct {
	// Endpoint is the address of the API. The tests give a stand-in's.
	Endpoint string
	// Token is the API token of the project, which is the whole project's
	// key.
	Token config.Secret
	// Labels mark what the adapter creates, and select what it finds.
	Labels map[string]string
	// PollInterval is the pause between two reads of an action that runs.
	// Zero keeps the one of hcloud-go.
	PollInterval time.Duration
}

// Provider reaches the servers of one environment at Hetzner.
type Provider struct {
	client   *hcloud.Client
	selector string
}

var _ provider.Provider = (*Provider)(nil)

// New gives the adapter.
func New(settings Settings) *Provider {
	options := []hcloud.ClientOption{
		hcloud.WithEndpoint(settings.Endpoint),
		hcloud.WithToken(settings.Token.Reveal()),
	}
	if settings.PollInterval > 0 {
		options = append(options, hcloud.WithPollOpts(hcloud.PollOpts{BackoffFunc: hcloud.ConstantBackoff(settings.PollInterval)}))
	}
	return &Provider{client: hcloud.NewClient(options...), selector: selectorOf(settings.Labels)}
}

// selectorOf writes the labels as a selector that matches all of them.
func selectorOf(labels map[string]string) string {
	terms := make([]string, 0, len(labels))
	for _, key := range slices.Sorted(maps.Keys(labels)) {
		terms = append(terms, key+"="+labels[key])
	}
	return strings.Join(terms, ",")
}

// Create registers the deploy key, creates the server with it and waits
// until Hetzner says the server runs. A key or a server with that name
// already is provider.ErrNameTaken. When the server cannot be created, the
// key is removed again.
func (p *Provider) Create(ctx context.Context, specification provider.Specification) (provider.Server, error) {
	key, _, err := p.client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      specification.Name,
		PublicKey: specification.DeployPublicKey,
		Labels:    specification.Labels,
	})
	if err != nil {
		return provider.Server{}, fmt.Errorf("register the deploy key: %w", named(err))
	}
	result, _, err := p.client.Server.Create(ctx, hcloud.ServerCreateOpts{
		Name:             specification.Name,
		ServerType:       &hcloud.ServerType{Name: specification.Size},
		Image:            &hcloud.Image{Name: specification.SystemImage},
		Location:         &hcloud.Location{Name: specification.Region},
		SSHKeys:          []*hcloud.SSHKey{key},
		UserData:         specification.CloudInit.Reveal(),
		StartAfterCreate: new(true),
		Labels:           specification.Labels,
		PublicNet:        &hcloud.ServerCreatePublicNet{EnableIPv4: true, EnableIPv6: false},
	})
	if err != nil {
		_, removeErr := p.client.SSHKey.Delete(context.WithoutCancel(ctx), key)
		return provider.Server{}, errors.Join(fmt.Errorf("create the server: %w", named(err)), removeErr)
	}
	actions := append([]*hcloud.Action{result.Action}, result.NextActions...)
	if err := p.client.Action.WaitFor(ctx, actions...); err != nil {
		return provider.Server{}, fmt.Errorf("wait until the server runs: %w", err)
	}
	created, _, err := p.client.Server.GetByID(ctx, result.Server.ID)
	if err != nil {
		return provider.Server{}, fmt.Errorf("read the server: %w", err)
	}
	if created == nil {
		return provider.Server{}, errors.New("read the server: Hetzner no longer has it")
	}
	return serverOf(created)
}

// named turns Hetzner's refusal of a name in use into
// provider.ErrNameTaken.
func named(err error) error {
	if hcloud.IsError(err, hcloud.ErrorCodeUniquenessError) {
		return fmt.Errorf("%w: %w", provider.ErrNameTaken, err)
	}
	return err
}

// Find gives the server with that name and the labels.
func (p *Provider) Find(ctx context.Context, name string) (provider.Server, bool, error) {
	servers, err := p.servers(ctx, name)
	if err != nil {
		return provider.Server{}, false, err
	}
	if len(servers) == 0 {
		return provider.Server{}, false, nil
	}
	server, err := serverOf(servers[0])
	return server, err == nil, err
}

// Destroy deletes the server with that name and the labels, waits until it
// is gone, deletes its IPv4 address if Hetzner kept it, and removes the
// deploy key registered under the name.
func (p *Provider) Destroy(ctx context.Context, name string) error {
	servers, err := p.servers(ctx, name)
	if err != nil {
		return err
	}
	var failed error
	for _, server := range servers {
		failed = errors.Join(failed, p.deleteServer(ctx, server))
	}
	keys, err := p.client.SSHKey.AllWithOpts(ctx, hcloud.SSHKeyListOpts{ListOpts: hcloud.ListOpts{LabelSelector: p.selector}, Name: name})
	if err != nil {
		return errors.Join(failed, fmt.Errorf("find the deploy key: %w", err))
	}
	for _, key := range keys {
		failed = errors.Join(failed, p.deleteKey(ctx, key))
	}
	return failed
}

func (p *Provider) servers(ctx context.Context, name string) ([]*hcloud.Server, error) {
	servers, err := p.client.Server.AllWithOpts(ctx, hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{LabelSelector: p.selector}, Name: name})
	if err != nil {
		return nil, fmt.Errorf("list the servers: %w", err)
	}
	return servers, nil
}

// deleteServer deletes a server and then its IPv4 address. Hetzner deletes
// an address it created with the server, unless it was told to keep it;
// this one is deleted either way, so that nothing is paid for afterwards.
func (p *Provider) deleteServer(ctx context.Context, server *hcloud.Server) error {
	result, _, err := p.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("delete the server %s: %w", server.Name, err)
	}
	if err := p.client.Action.WaitFor(ctx, result.Action); err != nil {
		return fmt.Errorf("wait until the server %s is deleted: %w", server.Name, err)
	}
	address := server.PublicNet.IPv4.ID
	if address == 0 {
		return nil
	}
	kept, _, err := p.client.PrimaryIP.GetByID(ctx, address)
	if err != nil {
		return fmt.Errorf("look for the address of the server %s: %w", server.Name, err)
	}
	if kept == nil {
		return nil
	}
	if _, err := p.client.PrimaryIP.Delete(ctx, kept); err != nil {
		return fmt.Errorf("delete the address of the server %s: %w", server.Name, err)
	}
	return nil
}

func (p *Provider) deleteKey(ctx context.Context, key *hcloud.SSHKey) error {
	if _, err := p.client.SSHKey.Delete(ctx, key); err != nil {
		return fmt.Errorf("remove the deploy key %s: %w", key.Name, err)
	}
	return nil
}

// serverOf reads what the tool needs of a server.
func serverOf(server *hcloud.Server) (provider.Server, error) {
	address, valid := netip.AddrFromSlice(server.PublicNet.IPv4.IP)
	if !valid {
		return provider.Server{}, fmt.Errorf("the server %s has no IPv4 address", server.Name)
	}
	return provider.Server{Name: server.Name, Address: address.Unmap(), Created: server.Created}, nil
}
