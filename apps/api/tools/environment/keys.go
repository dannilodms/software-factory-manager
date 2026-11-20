package main

import (
	"crypto/ed25519"
	"encoding/pem"
	"net"
	"net/netip"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
)

// serverKeys are the two key pairs a new server is made with, both drawn
// anew on each turn-on: the deploy key logs in as the deploy user, and the
// host key is the one the server answers SSH with, known before the first
// connection. A private key keeps the type of a secret, which prints as a
// placeholder, until the file or the program that uses it.
type serverKeys struct {
	// deployPrivate is the private deploy key in OpenSSH's format, the one
	// the local copy keeps.
	deployPrivate config.Secret
	// deployPublic is the public deploy key as a line of authorized keys.
	deployPublic string
	deploySigner ssh.Signer
	// hostPrivate is the private host key in OpenSSH's format, the one the
	// cloud-init file gives the server.
	hostPrivate config.Secret
	hostPublic  ssh.PublicKey
}

func newServerKeys(name string) (serverKeys, error) {
	_, deploy, err := ed25519.GenerateKey(nil)
	if err != nil {
		return serverKeys{}, err
	}
	_, host, err := ed25519.GenerateKey(nil)
	if err != nil {
		return serverKeys{}, err
	}
	deployPrivate, err := ssh.MarshalPrivateKey(deploy, "deploy@"+name)
	if err != nil {
		return serverKeys{}, err
	}
	hostPrivate, err := ssh.MarshalPrivateKey(host, "")
	if err != nil {
		return serverKeys{}, err
	}
	deploySigner, err := ssh.NewSignerFromKey(deploy)
	if err != nil {
		return serverKeys{}, err
	}
	hostPublic, err := ssh.NewPublicKey(host.Public())
	if err != nil {
		return serverKeys{}, err
	}
	return serverKeys{
		deployPrivate: config.NewSecret(string(pem.EncodeToMemory(deployPrivate))),
		deployPublic:  authorizedLine(deploySigner.PublicKey()),
		deploySigner:  deploySigner,
		hostPrivate:   config.NewSecret(string(pem.EncodeToMemory(hostPrivate))),
		hostPublic:    hostPublic,
	}, nil
}

// login is what logs in to the server made with these keys.
func (k serverKeys) login() loginKeys {
	return loginKeys{deploy: k.deploySigner, deployPrivate: k.deployPrivate, host: k.hostPublic}
}

// loginKeys log in to a server: the deploy key, and the only host key
// trusted. They travel together, and the address of the server beside them.
type loginKeys struct {
	deploy ssh.Signer
	// deployPrivate is the same deploy key in OpenSSH's format, as the
	// local copy and GitHub keep it.
	deployPrivate config.Secret
	host          ssh.PublicKey
}

// at is how SSH reaches the server at that address with the keys: as the
// deploy user, trusting the host key alone.
func (k loginKeys) at(address netip.Addr) sshclient.Target {
	return sshclient.Target{
		Address: net.JoinHostPort(address.String(), sshPort),
		HostKey: k.host,
		User:    deployUser,
		Key:     k.deploy,
	}
}

// authorizedLine writes a public key as a line of authorized keys, with no
// line break.
func authorizedLine(key ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}
