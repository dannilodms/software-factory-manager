package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// choices are what an environment's server is made of, from the committed
// file deploy/server.<environment>.yaml.
type choices struct {
	// Provider names the adapter that reaches the provider.
	Provider string `yaml:"provider"`
	// Region is where the server runs, as the provider names it.
	Region string `yaml:"region"`
	// Size is the plan of the server, as the provider names it.
	Size string `yaml:"size"`
	// SystemImage is the system the server starts from, as the provider
	// names it.
	SystemImage string `yaml:"system_image"`
	// ServerName is the name of the server at the provider.
	ServerName string `yaml:"server_name"`
}

// choicesFile is where the choices of an environment are, relative to the
// root of the checkout.
func choicesFile(name string) string {
	return "deploy/server." + name + ".yaml"
}

// loadChoices reads the choices of an environment from the checkout.
func loadChoices(root, name string) (choices, error) {
	file := choicesFile(name)
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if errors.Is(err, fs.ErrNotExist) {
		return choices{}, fmt.Errorf("%w %q: %s does not exist", errUnknownEnvironment, name, file)
	}
	if err != nil {
		return choices{}, err
	}
	var chosen choices
	if err := yaml.Load(content, &chosen, yaml.WithKnownFields()); err != nil {
		return choices{}, fmt.Errorf("read %s: %w", file, err)
	}
	return chosen, chosen.check(file)
}

// check fails when a choice is missing or names a provider the tool has no
// adapter for.
func (c choices) check(file string) error {
	fields := []struct{ key, value string }{
		{"provider", c.Provider},
		{"region", c.Region},
		{"size", c.Size},
		{"system_image", c.SystemImage},
		{"server_name", c.ServerName},
	}
	empty := slices.IndexFunc(fields, func(field struct{ key, value string }) bool { return field.value == "" })
	if empty >= 0 {
		return fmt.Errorf("%s: %s is missing", file, fields[empty].key)
	}
	if _, known := adapters[c.Provider]; !known {
		names := slices.Sorted(maps.Keys(adapters))
		return fmt.Errorf("%s: the tool has no adapter for the provider %q, only for %s", file, c.Provider, strings.Join(names, ", "))
	}
	return nil
}
