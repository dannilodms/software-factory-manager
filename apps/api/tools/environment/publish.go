package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
	"golang.org/x/crypto/ssh"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
)

// What the pipeline gives a publish, in the variables of its process: the
// commit and the pull request of the event, and what reaches the server,
// under the names the GitHub Environment holds it by (values.go).
const (
	// commitInput is the full hash of the commit whose images are published,
	// the tag the pipeline pushed them with. The variables file names it
	// too, so that the Compose file runs those images.
	commitInput = "COMMIT"
	// pullRequestInput is the number of the pull request published.
	pullRequestInput = "PULL_REQUEST"
)

// publishInputs are every variable a publish requires.
var publishInputs = []string{commitInput, pullRequestInput, addressVariable, hostKeyVariable, deployKeySecret, mailpitPasswordSecret}

const (
	// upDeadline is how long the app of a server has to answer once its
	// services started, which includes getting its certificates the first
	// time.
	upDeadline = 5 * time.Minute
	// variablesFileName is the variables file beside the Compose files on the
	// server, the `.env` of the Compose project, so that a command the
	// maintainer runs there by hand reads it too.
	variablesFileName = ".env"
	// mailpitUser is the user the browser is asked for on the host name of
	// Mailpit, as deploy/Caddyfile.staging names it.
	mailpitUser = "maintainer"
	// filesMode is the mode of a committed file copied to the server, and
	// variablesMode the one of the variables file, which only the deploy
	// user reads.
	filesMode     fs.FileMode = 0o644
	variablesMode fs.FileMode = 0o600
)

// recreatedVolumes are the volumes of the Compose project that every
// publish removes, so that staging starts from the seed's data again: those
// of PostgreSQL and Garage. Mailpit has none: its e-mails go with its
// container. Caddy's, which hold its certificates, stay.
var recreatedVolumes = []string{"postgres-data", "garage-data"}

// fullHash is the hash of a commit, as Git writes it in full.
var fullHash = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// remoteServer is the server of an environment, which a publish works on.
// A command carries no value: a value travels as the content of a file.
type remoteServer interface {
	// WriteFile writes the file at that path, relative to the home of the
	// deploy user, with that mode, creating its folder.
	WriteFile(ctx context.Context, name string, content []byte, mode fs.FileMode) error
	// Run runs the command and fails when it exits with a status other
	// than 0.
	Run(ctx context.Context, command string) error
	Close() error
}

// appReadiness waits until the app of a server answers.
type appReadiness interface {
	WaitUntilUp(ctx context.Context, url string) error
}

// publication is what a publish is given: the commit of a pull request, and
// what reaches the server, its address and the keys that log in to it.
type publication struct {
	commit          string
	pullRequest     int
	address         netip.Addr
	keys            loginKeys
	mailpitPassword config.Secret
}

// serverFile is a committed file of deploy/ that the Compose file of the
// environment reads, copied beside it on the server.
type serverFile struct {
	name    string
	content []byte
}

// step is one step of the publish, as the spec numbers them.
type step struct {
	what string
	do   func() error
}

// publish puts the images of one commit of a pull request on the server of
// the environment and starts the system there again from the seed's data,
// in the order that Publishing in CODING_STANDARDS.md gives. Every value it
// needs is checked before it logs in, so that a missing one never
// half-changes the server. It prints no value, no variables file and no
// configuration of a container.
//
// The label of the environment says which pull request it shows, and is
// read again first: a pull request that lost it, or was closed, since the
// event that started the run is not published.
func publish(ctx context.Context, environment environment, stdout io.Writer) error {
	given, err := environment.publication()
	if err != nil {
		return err
	}
	labelled, err := environment.stillLabelled(ctx, given.pullRequest)
	if err != nil {
		return err
	}
	if !labelled {
		fmt.Fprintf(stdout, "Pull request #%d does not have the %s label any more, so it is not published.\n", given.pullRequest, environment.name)
		return nil
	}
	variables, err := environment.variablesFile(given)
	if err != nil {
		return err
	}
	files, project, err := environment.serverFiles()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Publishing commit %s of pull request #%d to %s, at %s.\n",
		given.commit, given.pullRequest, environment.name, given.address)
	fmt.Fprintf(stdout, "Every name of .env.example has a value for %s.\n", environment.name)

	fmt.Fprintf(stdout, "1. Log in to the server as %s, trusting only the host key %s was given.\n", deployUser, environment.name)
	server, err := environment.outside.reachServer(ctx, given.keys.at(given.address), stdout)
	if err != nil {
		return fmt.Errorf("step 1, log in to the server at %s: %w", given.address, err)
	}
	defer server.Close()
	if err := inSteps(stdout, environment.steps(ctx, server, given, files, variables, project)); err != nil {
		return fmt.Errorf("%w. A new publish starts %s again from the beginning", err, environment.name)
	}
	return environment.announce(ctx, given, stdout)
}

// steps are the steps of the publish once it is logged in, from the second.
func (e environment) steps(ctx context.Context, server remoteServer, given publication, files []serverFile, variables []byte, project string) []step {
	compose := fmt.Sprintf("docker compose --file %s --env-file %s", e.onServer("compose."+e.name+".yaml"), e.onServer(variablesFileName))
	running := func(commands ...string) func() error {
		return func() error {
			each := make([]func() error, 0, len(commands))
			for _, command := range commands {
				each = append(each, func() error { return server.Run(ctx, command) })
			}
			return inOrder(each...)
		}
	}
	copyFiles := func() error {
		each := make([]func() error, 0, len(files))
		for _, file := range files {
			each = append(each, func() error { return server.WriteFile(ctx, e.onServer(file.name), file.content, filesMode) })
		}
		return inOrder(each...)
	}
	volumes := make([]string, 0, len(recreatedVolumes))
	for _, volume := range recreatedVolumes {
		volumes = append(volumes, project+"_"+volume)
	}
	up := "https://" + hostNamesOf(given.address).app + "/up"
	return []step{
		{"copy the Compose files and the configuration of Caddy", copyFiles},
		{"write the variables file, readable by the deploy user alone", func() error {
			return server.WriteFile(ctx, e.onServer(variablesFileName), variables, variablesMode)
		}},
		{"pull the images of the commit", running(compose + " pull --quiet")},
		{"stop the API and the worker", running(compose + " stop api worker")},
		// Mailpit keeps its e-mails inside its container, which goes with
		// the others, so that no login code of an earlier pull request is
		// left.
		{"remove PostgreSQL, Garage and Mailpit, with their data", running(
			compose+" rm --stop --force postgres garage mailpit",
			"docker volume rm --force "+strings.Join(volumes, " "),
		)},
		// Each service from here on is started by its own step, in this
		// order, so none starts the ones it depends on again (--no-deps).
		{"start PostgreSQL, Garage and its rule, Mailpit and Caddy", running(compose + " up --detach --wait postgres garage garage-cors mailpit caddy")},
		{"run the migration, then the seed", running(
			compose+" run --rm --no-deps -T migrate",
			compose+" run --rm --no-deps -T seed",
		)},
		{"start the API and the worker", running(compose + " up --detach --wait --no-deps api worker")},
		{fmt.Sprintf("wait until %s answers, for up to %v", up, upDeadline), func() error {
			ctx, cancel := context.WithTimeout(ctx, upDeadline)
			defer cancel()
			return e.outside.app.WaitUntilUp(ctx, up)
		}},
	}
}

// inSteps runs the steps in order, numbered from 2, and stops at the first
// that fails.
func inSteps(stdout io.Writer, steps []step) error {
	var failed error
	for i := 0; i < len(steps) && failed == nil; i++ {
		fmt.Fprintf(stdout, "%d. %s.\n", i+2, strings.ToUpper(steps[i].what[:1])+steps[i].what[1:])
		failed = failedTo(fmt.Sprintf("step %d, %s", i+2, steps[i].what), steps[i].do())
	}
	return failed
}

// announce says where the environment shows the pull request, takes the
// label of the environment off every other open pull request that has it,
// with a comment there, and comments the addresses on the one it shows. It
// tries each of them, and then fails with whatever failed.
func (e environment) announce(ctx context.Context, given publication, stdout io.Writer) error {
	names := hostNamesOf(given.address)
	fmt.Fprintf(stdout, "%s shows pull request #%d.\n  App:    https://%s\n  Files:  https://%s\n  Mail:   https://%s, as the user %s\n",
		e.title(), given.pullRequest, names.app, names.files, names.mail, mailpitUser)
	failed := errors.Join(
		e.takeTheLabelOffTheOthers(ctx, given.pullRequest, stdout),
		e.commentTheAddresses(ctx, given, stdout),
	)
	if failed != nil {
		// errors.Join writes one failure a line: the output gives them one
		// line together.
		return fmt.Errorf("%s shows pull request #%d, but %s", e.name, given.pullRequest, strings.ReplaceAll(failed.Error(), "\n", "; "))
	}
	return nil
}

// stillLabelled tells whether the pull request with that number is open and
// has the label of the environment now.
func (e environment) stillLabelled(ctx context.Context, number int) (bool, error) {
	labelled, err := e.outside.github.LabelledPullRequests(ctx, e.name)
	if err != nil {
		return false, fmt.Errorf("find whether pull request #%d still has the %s label: %w", number, e.name, err)
	}
	return slices.ContainsFunc(labelled, func(open github.PullRequest) bool { return open.Number == number }), nil
}

// takeTheLabelOffTheOthers takes the label of the environment off every
// other open pull request that has it, so that a push to one of them
// publishes nothing, with a comment there naming the pull request the
// environment shows now. It tries each of them.
func (e environment) takeTheLabelOffTheOthers(ctx context.Context, shown int, stdout io.Writer) error {
	labelled, err := e.outside.github.LabelledPullRequests(ctx, e.name)
	if err != nil {
		return fmt.Errorf("the open pull requests with the %s label could not be read: %w", e.name, err)
	}
	var failed error
	for _, other := range labelled {
		failed = errors.Join(failed, e.takeTheLabelOff(ctx, other.Number, shown, stdout))
	}
	return failed
}

// takeTheLabelOff takes the label of the environment off the pull request
// with that number, unless it is the one the environment shows.
func (e environment) takeTheLabelOff(ctx context.Context, number, shown int, stdout io.Writer) error {
	if number == shown {
		return nil
	}
	if err := e.outside.github.RemoveLabel(ctx, number, e.name); err != nil {
		return fmt.Errorf("the %s label is still on pull request #%d: %w", e.name, number, err)
	}
	if err := e.outside.github.CommentOnPullRequest(ctx, number, e.shownElsewhereComment(shown)); err != nil {
		return fmt.Errorf("pull request #%d lost the %s label, with no comment saying why: %w", number, e.name, err)
	}
	fmt.Fprintf(stdout, "Took the %s label off pull request #%d, with a comment there.\n", e.name, number)
	return nil
}

// commentTheAddresses comments the addresses on the pull request the
// environment shows.
func (e environment) commentTheAddresses(ctx context.Context, given publication, stdout io.Writer) error {
	if err := e.outside.github.CommentOnPullRequest(ctx, given.pullRequest, e.publishedComment(given)); err != nil {
		return fmt.Errorf("it got no comment with the addresses: %w", err)
	}
	fmt.Fprintf(stdout, "Commented the addresses on pull request #%d.\n", given.pullRequest)
	return nil
}

// shownElsewhereComment is the comment of a pull request whose label came
// off because the environment shows another one now.
func (e environment) shownElsewhereComment(shown int) string {
	return fmt.Sprintf("%s shows pull request #%d now, so the `%s` label came off this pull request, "+
		"and a push to it publishes nothing. Add the label again to publish it.\n", e.title(), shown, e.name)
}

// publishedComment is the comment of the pull request that the environment
// shows: the addresses of the app and of Mailpit, and how to log in.
func (e environment) publishedComment(given publication) string {
	names := hostNamesOf(given.address)
	return fmt.Sprintf("%s shows this pull request, at commit %s.\n\n"+
		"- The app: https://%s\n"+
		"- Mailpit: https://%s, as the user `%s`, with the password `%s` of the `.env` of the main checkout\n\n"+
		"Its data is the seed's: log in as `%s` with a login code, which Mailpit receives.\n",
		e.title(), given.commit, names.app, names.mail, mailpitUser, mailpitPasswordName(e.name), seedAdministrator())
}

// onServer is where a file of the environment is on its server: in a
// folder of the deploy user's home named after the environment.
func (e environment) onServer(name string) string {
	return e.name + "/" + name
}

// publication reads what the pipeline gave the publish. A missing or
// unreadable value is named, never printed.
func (e environment) publication() (publication, error) {
	missing := slices.DeleteFunc(slices.Clone(publishInputs), func(name string) bool { return e.variables[name] != "" })
	if len(missing) > 0 {
		return publication{}, fmt.Errorf("the publish needs %s, which the pipeline gives it from the pull request and from the GitHub Environment %s",
			strings.Join(missing, ", "), e.name)
	}
	commit := e.variables[commitInput]
	if !fullHash.MatchString(commit) {
		return publication{}, fmt.Errorf("%s is not the full hash of a commit", commitInput)
	}
	pullRequest, err := strconv.Atoi(e.variables[pullRequestInput])
	if err != nil || pullRequest < 1 {
		return publication{}, fmt.Errorf("%s is not the number of a pull request", pullRequestInput)
	}
	address, err := netip.ParseAddr(e.variables[addressVariable])
	if err != nil || !address.Is4() {
		return publication{}, fmt.Errorf("%s is not an IPv4 address", addressVariable)
	}
	hostKey, err := hostKeyOf(e.variables[hostKeyVariable], address)
	if err != nil {
		return publication{}, err
	}
	// gh keeps the key without its last line break, which the reader of
	// PEM does without.
	deployPrivate := config.NewSecret(e.variables[deployKeySecret])
	deploy, err := ssh.ParsePrivateKey([]byte(deployPrivate.Reveal()))
	if err != nil {
		return publication{}, fmt.Errorf("%s is not a private key in OpenSSH's format", deployKeySecret)
	}
	return publication{
		commit:          commit,
		pullRequest:     pullRequest,
		address:         address,
		keys:            loginKeys{deploy: deploy, deployPrivate: deployPrivate, host: hostKey},
		mailpitPassword: config.NewSecret(e.variables[mailpitPasswordSecret]),
	}, nil
}

// hostKeyOf reads the host key from its line of known hosts, which must
// name the address.
func hostKeyOf(line string, address netip.Addr) (ssh.PublicKey, error) {
	_, hosts, key, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil || !slices.Contains(hosts, address.String()) {
		return nil, fmt.Errorf("%s is not a line of known hosts for %s", hostKeyVariable, addressVariable)
	}
	return key, nil
}

// serverFiles reads the committed files the server of the environment
// needs, and the name of its Compose project, from its Compose file.
func (e environment) serverFiles() ([]serverFile, string, error) {
	names := []string{"compose.base.yaml", "compose." + e.name + ".yaml", "Caddyfile." + e.name, "delivery.caddyfile"}
	files := make([]serverFile, 0, len(names))
	var failed error
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(e.checkout.root, "deploy", name))
		failed = errors.Join(failed, err)
		files = append(files, serverFile{name: name, content: content})
	}
	if failed != nil {
		return nil, "", fmt.Errorf("read the files of the server: %w", failed)
	}
	var project struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(files[1].content, &project); err != nil || project.Name == "" {
		return nil, "", fmt.Errorf("deploy/%s names no Compose project", names[1])
	}
	return files, project.Name, nil
}
