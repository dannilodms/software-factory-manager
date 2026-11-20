package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
)

// errUnknownEnvironment is the answer for a name that no choices file
// describes.
var errUnknownEnvironment = errors.New("unknown environment")

// environmentName is what the name of an environment may be: it names a
// file of deploy/ and a folder of the main checkout.
var environmentName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// environment is the environment a command works on.
type environment struct {
	name      string
	choices   choices
	checkout  checkout
	variables map[string]string
	outside   outside
}

// open finds the checkout the command runs in and reads the choices of the
// environment.
func open(name string, variables map[string]string, outside outside) (environment, error) {
	if !environmentName.MatchString(name) {
		return environment{}, fmt.Errorf("%w %q", errUnknownEnvironment, name)
	}
	found, err := findCheckout(outside.directory)
	if err != nil {
		return environment{}, err
	}
	chosen, err := loadChoices(found.root, name)
	if err != nil {
		return environment{}, err
	}
	return environment{name: name, choices: chosen, checkout: found, variables: variables, outside: outside}, nil
}

// title is the name of the environment at the start of a sentence.
func (e environment) title() string {
	return strings.ToUpper(e.name[:1]) + e.name[1:]
}

// provider connects to the provider of the environment, with the token its
// adapter reads.
func (e environment) provider() (provider.Provider, error) {
	adapter := adapters[e.choices.Provider]
	token, err := e.secret(adapter.tokenVariable)
	if err != nil {
		return nil, err
	}
	if token.Reveal() == "" {
		return nil, fmt.Errorf("%s is required to reach %s: write it in the .env of the main checkout, %s",
			adapter.tokenVariable, e.choices.Provider, e.checkout.envFile())
	}
	return e.outside.connect(e.choices.Provider, token, e.labels()), nil
}

// labels mark what the provider creates for the environment.
func (e environment) labels() map[string]string {
	return map[string]string{"environment": e.name}
}

// secret reads a secret of the tool from a variable of the process, or else
// of the .env of the main checkout. In the main checkout Task has loaded
// that file already; in a worktree it loaded the worktree's own, which has
// none of the tool's. A secret that is set nowhere is empty.
func (e environment) secret(name string) (config.Secret, error) {
	if value := e.variables[name]; value != "" {
		return config.NewSecret(value), nil
	}
	value, err := valueIn(e.checkout.envFile(), name)
	if err != nil {
		return config.Secret{}, fmt.Errorf("read %s from %s: %w", name, e.checkout.envFile(), err)
	}
	return config.NewSecret(value), nil
}

// localCopy is where the main checkout keeps the keys of the environment's
// server while it is on.
func (e environment) localCopy() localCopy {
	return localCopy{folder: filepath.Join(e.checkout.main, localCopiesFolder, e.name), host: e.name}
}
