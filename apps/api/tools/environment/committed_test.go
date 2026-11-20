package main

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
)

// The tests of the committed files of deploy/ that the tool reads: what the
// first real turn-on would otherwise be the first to find.

// dockerPackages are the packages of Docker that the cloud-init file
// installs from Docker's repository, each at the version it names.
var dockerPackages = []string{"containerd.io", "docker-ce-cli", "docker-ce", "docker-compose-plugin"}

// committedCloudInit is the part of the cloud-init file these tests read.
type committedCloudInit struct {
	Packages   []any         `yaml:"packages"`
	RunCmd     []string      `yaml:"runcmd"`
	WriteFiles []writtenFile `yaml:"write_files"`
}

// writtenFile is a file the cloud-init file writes on the server.
type writtenFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

func readCommittedCloudInit(t *testing.T) committedCloudInit {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repository, "deploy", "cloud-init.yaml"))
	require.NoError(t, err)
	var file committedCloudInit
	require.NoError(t, yaml.Unmarshal(content, &file), "the committed file, before the tool fills it, is YAML")
	return file
}

// pinned gives the version of each package, empty for one with none.
func pinned(t *testing.T, file committedCloudInit) map[string]string {
	t.Helper()
	versions := map[string]string{}
	for _, entry := range file.Packages {
		name, version := packageOf(t, entry)
		versions[name] = version
	}
	return versions
}

// packageOf reads an entry of the packages: a name, or [name, version].
func packageOf(t *testing.T, entry any) (string, string) {
	t.Helper()
	pair, isPair := entry.([]any)
	if !isPair {
		return fmt.Sprint(entry), ""
	}
	require.Len(t, pair, 2, "a package with a version is [name, version]")
	return fmt.Sprint(pair[0]), fmt.Sprint(pair[1])
}

func TestTheCloudInitFileInstallsDockerAtExactVersionsBuiltForTheSystemImageOfEveryEnvironment(t *testing.T) {
	t.Parallel()
	versions := pinned(t, readCommittedCloudInit(t))
	files, err := filepath.Glob(filepath.Join(repository, "deploy", "server.*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, file := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "server."), ".yaml")
		assertDockerBuiltForTheSystemImageOf(t, name, versions)
	}
}

func assertDockerBuiltForTheSystemImageOf(t *testing.T, name string, versions map[string]string) {
	t.Helper()
	chosen, err := loadChoices(repository, name)
	require.NoError(t, err)
	release := regexp.MustCompile(`^ubuntu-(\d\d\.\d\d)$`).FindStringSubmatch(chosen.SystemImage)
	require.NotNil(t, release, "the system image of %s names a release of Ubuntu", name)
	exact := `^(\d+:)?\d+\.\d+\.\d+-\d+~ubuntu\.` + regexp.QuoteMeta(release[1]) + `~[a-z]+$`
	for _, docker := range dockerPackages {
		assert.Regexp(t, exact, versions[docker],
			"%s is installed at an exact version built for Ubuntu %s, the system image of %s", docker, release[1], name)
	}
}

func TestTheCloudInitFileStopsAtTheFirstFailureAndSignalsLastThatTheServerIsPrepared(t *testing.T) {
	t.Parallel()
	steps := readCommittedCloudInit(t).RunCmd

	require.NotEmpty(t, steps)
	assert.Equal(t, "set -eu", steps[0], "the steps are one script, which stops at the first one that fails")
	assert.Equal(t, "touch "+sshclient.PreparedSignal, steps[len(steps)-1], "the last step writes the signal readiness waits for")
	assert.True(t, slices.Contains(steps, "ufw --force enable"))
	assert.Less(t, slices.Index(steps, "ufw allow 22/tcp"), slices.Index(steps, "ufw --force enable"), "SSH is let in before the firewall starts")
}

// containersWithoutMetadata is the unit of systemd that keeps every
// container from the metadata service of the provider, which serves the
// user data of the server, the private host key among it.
const containersWithoutMetadata = "containers-without-metadata.service"

func TestTheCloudInitFileKeepsTheContainersFromTheMetadataServiceBeforeDockerStartsOne(t *testing.T) {
	t.Parallel()
	file := readCommittedCloudInit(t)

	found := slices.IndexFunc(file.WriteFiles, func(written writtenFile) bool {
		return written.Path == "/etc/systemd/system/"+containersWithoutMetadata
	})
	require.GreaterOrEqual(t, found, 0, "the cloud-init file writes the unit %s", containersWithoutMetadata)
	unit := file.WriteFiles[found].Content
	// Docker sends what a container sends through the chain DOCKER-USER,
	// which it creates when it is missing and never changes.
	assert.Contains(t, unit, "ExecStart=/usr/sbin/iptables -I DOCKER-USER -d 169.254.169.254/32 -j DROP")
	assert.Contains(t, unit, "Before=docker.service", "on every boot, the rule is in place before Docker starts a container")
	assert.Contains(t, unit, "After=ufw.service", "ufw loads its rules first")
	assert.Contains(t, unit, "RequiredBy=docker.service", "Docker starts no container without the rule")
	steps := file.RunCmd
	enabled := slices.Index(steps, "systemctl enable --now "+containersWithoutMetadata)
	require.GreaterOrEqual(t, enabled, 0, "the first boot puts the rule in place too")
	assert.Less(t, slices.Index(steps, "ufw --force enable"), enabled, "after ufw has loaded its rules")
	assert.Less(t, slices.Index(steps, "systemctl daemon-reload"), enabled, "systemd reads the unit before it enables it")
}

func TestTheRenderedCloudInitFileFitsTheUserDataOfAServer(t *testing.T) {
	t.Parallel()
	keys, err := newServerKeys("staging")
	require.NoError(t, err)

	rendered, err := renderCloudInit(repository, keys)

	require.NoError(t, err)
	assert.Less(t, len(rendered.Reveal()), 32*1024, "Hetzner takes 32 KiB of user data")
	assert.NotContains(t, rendered.Reveal(), "{{")
}

func TestEveryStagingValueIsOfANameOfEnvExample(t *testing.T) {
	t.Parallel()
	names, err := namesIn(filepath.Join(repository, ".env.example"))
	require.NoError(t, err)

	for name := range stagingValues {
		assert.Contains(t, names, name, "the staging value of %s is of no name of .env.example: remove it", name)
	}
}

// required finds a variable that Compose requires: ${NAME:?message}.
var required = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*):\?`)

func TestEveryVariableTheComposeFilesOfStagingRequireHasAValueOnEveryPublish(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.succeeds("publish", "staging")
	values := world.variablesOnServer()

	for _, file := range []string{"compose.staging.yaml", "compose.base.yaml"} {
		assertEveryRequiredVariableHasAValue(t, file, values)
	}
}

func assertEveryRequiredVariableHasAValue(t *testing.T, file string, values map[string]string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repository, "deploy", file))
	require.NoError(t, err)
	for _, found := range required.FindAllStringSubmatch(string(content), -1) {
		assert.NotEmpty(t, values[found[1]], "deploy/%s requires %s, which the publish leaves empty", file, found[1])
	}
}

// composeService is the part of a service of a Compose file that the test of
// what each container receives reads.
type composeService struct {
	Extends struct {
		Service string `yaml:"service"`
	} `yaml:"extends"`
	Image       string            `yaml:"image"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
	Healthcheck struct {
		Test []string `yaml:"test"`
	} `yaml:"healthcheck"`
}

func readComposeServices(t *testing.T, file string) map[string]composeService {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repository, "deploy", file))
	require.NoError(t, err)
	var read struct {
		Services map[string]composeService `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(content, &read))
	return read.Services
}

// extending is the service as Compose runs it: what the service of the base
// file it extends is, with what it says itself on top.
func extending(base, service composeService) composeService {
	environment := maps.Clone(base.Environment)
	if environment == nil {
		environment = map[string]string{}
	}
	maps.Copy(environment, service.Environment)
	service.Environment = environment
	service.Image = cmp.Or(service.Image, base.Image)
	service.Command = ownOrBase(service.Command, base.Command)
	service.Healthcheck.Test = ownOrBase(service.Healthcheck.Test, base.Healthcheck.Test)
	return service
}

// ownOrBase is the list a service writes, or else the one of its base.
func ownOrBase(own, base []string) []string {
	if len(own) == 0 {
		return base
	}
	return own
}

// interpolation is where Compose puts a variable of the variables file:
// ${NAME}, ${NAME:-default} or ${NAME:?message}.
var interpolation = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)(?::([-?])([^}]*))?\}`)

// received is the environment the container of the service receives, with
// the values of the variables file.
func received(service composeService, values map[string]string) map[string]string {
	environment := make(map[string]string, len(service.Environment))
	for name, written := range service.Environment {
		environment[name] = interpolation.ReplaceAllStringFunc(written, func(found string) string {
			parts := interpolation.FindStringSubmatch(found)
			return cmp.Or(values[parts[1]], defaultOf(parts[2], parts[3]))
		})
	}
	return environment
}

// defaultOf is what Compose writes for an empty variable: the default of
// ${NAME:-default}, and nothing for ${NAME:?message}, which it refuses.
func defaultOf(operator, text string) string {
	if operator != "-" {
		return ""
	}
	return text
}

// The images of the binary of the API and of the seed, which deploy/
// compose.staging.yaml runs.
const (
	imageOfTheAPI  = "ghcr.io/dannilodms/software-factory-manager-api"
	imageOfTheSeed = "ghcr.io/dannilodms/software-factory-manager-seed"
)

// commandsOf gives the commands of the binary that the container of the
// service runs: the seed, or the command of the API binary it starts with,
// the API itself when it names none, and the one of its health check.
func commandsOf(service composeService) []config.Command {
	image, _, _ := strings.Cut(service.Image, ":")
	if image == imageOfTheSeed {
		return []config.Command{config.Seed}
	}
	if image != imageOfTheAPI {
		return nil
	}
	started := config.ServeAPI
	if len(service.Command) > 0 {
		started = config.Command(service.Command[0])
	}
	checked := service.Healthcheck.Test
	if len(checked) == 3 && checked[1] == "/api" {
		return []config.Command{started, config.Command(checked[2])}
	}
	return []config.Command{started}
}

// The configuration of the binary says what each of its commands requires:
// a variable decided in variables.go reaches a container only when the
// Compose file lists it in the environment of its service.
func TestEachContainerOfTheBinaryInStagingReceivesWhatItsCommandsRequire(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.succeeds("publish", "staging")
	values := world.variablesOnServer()
	base := readComposeServices(t, "compose.base.yaml")

	run := map[string][]config.Command{}
	for name, service := range readComposeServices(t, "compose.staging.yaml") {
		service = extending(base[service.Extends.Service], service)
		run[name] = commandsOf(service)
		assertEachCommandReceivesWhatItRequires(t, name, run[name], received(service, values))
	}

	maps.DeleteFunc(run, func(_ string, commands []config.Command) bool { return len(commands) == 0 })
	assert.Equal(t, map[string][]config.Command{
		"api":     {config.ServeAPI, config.ProbeReadiness},
		"worker":  {config.Worker},
		"migrate": {config.Migrate},
		"seed":    {config.Seed},
	}, run, "the services that run the binary, and the commands of each")
}

func assertEachCommandReceivesWhatItRequires(t *testing.T, service string, commands []config.Command, environment map[string]string) {
	t.Helper()
	for _, command := range commands {
		_, err := config.Load(command, environment)
		assert.NoError(t, err, "the environment of %s in deploy/compose.staging.yaml lacks what %s requires", service, command)
	}
}

func TestTheComposeFileOfStagingDeclaresTheVolumesThePublishRemovesAndTheOnesItKeeps(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join(repository, "deploy", "compose.staging.yaml"))
	require.NoError(t, err)
	var file struct {
		Name    string         `yaml:"name"`
		Volumes map[string]any `yaml:"volumes"`
	}
	require.NoError(t, yaml.Unmarshal(content, &file))

	assert.Equal(t, "software-factory-manager-staging", file.Name)
	assert.ElementsMatch(t, append(slices.Clone(recreatedVolumes), "caddy-data", "caddy-config"), slices.Collect(maps.Keys(file.Volumes)),
		"the publish removes the data of PostgreSQL and Garage and keeps the certificates of Caddy")
}

func TestTheCaddyfileOfStagingAsksForTheUserOfMailpitThatTheCommentNames(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join(repository, "deploy", "Caddyfile.staging"))
	require.NoError(t, err)

	assert.Regexp(t, `\n\s+`+mailpitUser+` \{\$MAILPIT_PASSWORD_HASH\}\n`, string(content))
}
