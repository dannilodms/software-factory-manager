package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
)

// localCopiesFolder is the folder of the main checkout, ignored by git, that
// keeps a folder for each environment that is on.
const localCopiesFolder = ".servers"

const (
	deployKeyFile  = "deploy-key"
	knownHostsFile = "known_hosts"
	sshConfigFile  = "ssh_config"
)

// localCopy is the folder of the main checkout that keeps, while an
// environment is on, what logs in to its server: the private deploy key,
// the host key as a line of known hosts, and a configuration of OpenSSH that
// uses both. Only the owner of the files can read them, which OpenSSH
// requires of a private key on every system.
type localCopy struct {
	folder string
	// host is the name the configuration of OpenSSH gives the server.
	host string
}

// save keeps the keys of the server at that address, replacing what was
// kept before.
func (l localCopy) save(address netip.Addr, keys loginKeys) error {
	if err := l.remove(); err != nil {
		return err
	}
	if err := os.MkdirAll(l.folder, 0o700); err != nil {
		return err
	}
	if err := restrictToOwner(l.folder); err != nil {
		return fmt.Errorf("restrict %s to its owner: %w", l.folder, err)
	}
	knownHosts := knownhosts.Line([]string{address.String()}, keys.host) + "\n"
	return errors.Join(
		writePrivate(l.path(deployKeyFile), []byte(keys.deployPrivate.Reveal())),
		writePrivate(l.path(knownHostsFile), []byte(knownHosts)),
		writePrivate(l.path(sshConfigFile), []byte(l.sshConfig(address))),
	)
}

// read gives the keys kept, and tells whether there are any.
func (l localCopy) read() (loginKeys, bool, error) {
	deployKey, err := os.ReadFile(l.path(deployKeyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return loginKeys{}, false, nil
	}
	if err != nil {
		return loginKeys{}, false, err
	}
	signer, err := ssh.ParsePrivateKey(deployKey)
	if err != nil {
		return loginKeys{}, false, fmt.Errorf("read %s: %w", l.path(deployKeyFile), err)
	}
	knownHosts, err := os.ReadFile(l.path(knownHostsFile))
	if err != nil {
		return loginKeys{}, false, err
	}
	_, _, hostKey, _, _, err := ssh.ParseKnownHosts(knownHosts)
	if err != nil {
		return loginKeys{}, false, fmt.Errorf("read %s: %w", l.path(knownHostsFile), err)
	}
	return loginKeys{deploy: signer, deployPrivate: config.NewSecret(string(deployKey)), host: hostKey}, true, nil
}

// kept tells whether there is a copy.
func (l localCopy) kept() (bool, error) {
	_, err := os.Stat(l.path(deployKeyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// remove removes the copy. Removing what does not exist succeeds.
func (l localCopy) remove() error {
	return os.RemoveAll(l.folder)
}

func (l localCopy) path(file string) string {
	return filepath.Join(l.folder, file)
}

// sshCommand is the command that logs in to the server with the copy.
func (l localCopy) sshCommand() string {
	return fmt.Sprintf(`ssh -F "%s" %s`, filepath.ToSlash(l.path(sshConfigFile)), l.host)
}

// sshConfig is a configuration of OpenSSH that logs in to the server at that
// address as the deploy user, with the deploy key, trusting its host key
// alone.
func (l localCopy) sshConfig(address netip.Addr) string {
	return fmt.Sprintf(`# The way in to the server of %[1]s, written when %[1]s was turned on and
# removed when it is turned off: %[2]s
Host %[1]s
  HostName %[3]s
  User %[4]s
  IdentityFile "%[5]s"
  IdentitiesOnly yes
  UserKnownHostsFile "%[6]s"
  StrictHostKeyChecking yes
`, l.host, l.sshCommand(), address, deployUser,
		filepath.ToSlash(l.path(deployKeyFile)), filepath.ToSlash(l.path(knownHostsFile)))
}

// writePrivate writes a file that only its owner can read.
func writePrivate(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	if err := restrictToOwner(path); err != nil {
		return fmt.Errorf("restrict %s to its owner: %w", path, err)
	}
	return nil
}
