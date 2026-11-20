package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
)

// cloudInitFile prepares every server, relative to the root of the
// checkout.
const cloudInitFile = "deploy/cloud-init.yaml"

// cloudInitValues are what the cloud-init file takes from a turn-on, where
// its {{ }} are. The private host key keeps the type of a secret: the file
// reveals it where it writes it.
type cloudInitValues struct {
	HostPrivateKey  config.Secret
	HostPublicKey   string
	DeployPublicKey string
}

// renderCloudInit writes the keys of a new server into the cloud-init file
// of the checkout. The result holds the private host key.
func renderCloudInit(root string, keys serverKeys) (config.Secret, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cloudInitFile)))
	if err != nil {
		return config.Secret{}, err
	}
	file, err := template.New(cloudInitFile).
		Funcs(template.FuncMap{"indent": indent}).
		Option("missingkey=error").
		Parse(string(content))
	if err != nil {
		return config.Secret{}, fmt.Errorf("read %s: %w", cloudInitFile, err)
	}
	values := cloudInitValues{
		HostPrivateKey:  keys.hostPrivate,
		HostPublicKey:   authorizedLine(keys.hostPublic),
		DeployPublicKey: keys.deployPublic,
	}
	var rendered strings.Builder
	if err := file.Execute(&rendered, values); err != nil {
		return config.Secret{}, fmt.Errorf("write the keys into %s: %w", cloudInitFile, err)
	}
	return config.NewSecret(rendered.String()), nil
}

// indent puts spaces before every line of the text but the first, which
// the file indents itself, so that the text stays inside a block of YAML.
func indent(spaces int, text string) string {
	return strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n"+strings.Repeat(" ", spaces))
}
