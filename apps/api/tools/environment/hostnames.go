package main

import (
	"fmt"
	"io"
	"net/netip"
	"strings"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
)

// report writes the host names of the server, and how to log in to it.
func report(stdout io.Writer, environment environment, server provider.Server) error {
	names := hostNamesOf(server.Address)
	fmt.Fprintf(stdout, "  App:    https://%s\n  Files:  https://%s\n  Mail:   https://%s\n", names.app, names.files, names.mail)
	kept, err := environment.localCopy().kept()
	if err != nil {
		return fmt.Errorf("look for the local copy of the deploy key: %w", err)
	}
	if !kept {
		fmt.Fprintln(stdout, "  SSH:    the main checkout keeps no copy of the deploy key")
		return nil
	}
	fmt.Fprintf(stdout, "  SSH:    %s\n", environment.localCopy().sshCommand())
	return nil
}

// hostNames are the host names of a server. Until the domain is registered
// they are under sslip.io, which answers a name that carries an address with
// that address (provisional, see docs/providers.md).
type hostNames struct {
	app, files, mail string
}

// hostNamesOf writes the address with dashes, as one label of the name.
func hostNamesOf(address netip.Addr) hostNames {
	domain := strings.ReplaceAll(address.String(), ".", "-") + ".sslip.io"
	return hostNames{app: "app." + domain, files: "files." + domain, mail: "mail." + domain}
}
