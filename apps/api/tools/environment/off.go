package main

import (
	"context"
	"fmt"
	"io"
)

// turnOff destroys the server of the environment, with its address and its
// deploy key at the provider, removes the local copy of the key and takes
// the values of the server out of the GitHub Environment, which keeps the
// password of Mailpit. When the environment is off already it succeeds and
// says so, and still takes out of GitHub what a turn-on cut short left
// there.
func turnOff(ctx context.Context, environment environment, stdout io.Writer) error {
	theProvider, err := environment.provider()
	if err != nil {
		return err
	}
	name := environment.choices.ServerName
	server, found, err := theProvider.Find(ctx, name)
	if err != nil {
		return fmt.Errorf("find the server %s at %s: %w", name, environment.choices.Provider, err)
	}
	// Even with no server: a turn-on cut short may have left its deploy key
	// at the provider.
	if err := theProvider.Destroy(ctx, name); err != nil {
		return fmt.Errorf("destroy the server %s at %s: %w", name, environment.choices.Provider, err)
	}
	if err := environment.localCopy().remove(); err != nil {
		return fmt.Errorf("remove the local copy of the deploy key: %w", err)
	}
	if err := environment.deleteValues(ctx); err != nil {
		return fmt.Errorf("the server %s was destroyed, but its values are still in GitHub (%w): turn %s off again", name, err, environment.name)
	}
	if !found {
		fmt.Fprintf(stdout, "%s was off already: there was no server to destroy.\n", environment.title())
	}
	if found {
		fmt.Fprintf(stdout, "%s is off: the server %s at %s was destroyed at %s, with its address and its deploy key.\n",
			environment.title(), name, server.Address, environment.choices.Provider)
	}
	fmt.Fprintf(stdout, "The GitHub Environment %s keeps only the password of Mailpit.\n", environment.name)
	return nil
}
