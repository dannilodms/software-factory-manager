// Command environment turns an environment of the system on and off from the
// maintainer's machine, tells its status, and publishes a pull request to
// it. Staging is the one environment it knows: turning it on creates its
// server at the provider, prepared by deploy/cloud-init.yaml, and turning it
// off destroys it (ADR-0019). Task runs it as `task staging:on`,
// `task staging:off` and `task staging:status`; the pipeline runs the
// publish, which logs in to the server over SSH and needs no token of the
// provider. It is built into no image.
//
// The choices of an environment's server are in deploy/server.<name>.yaml.
// The provider is an adapter: Hetzner's, today, whose token is HETZNER_TOKEN,
// read from the environment or else from the .env of the main checkout, so
// that the command runs from a worktree too. While an environment is on, the
// main checkout keeps a copy of its deploy key, outside git, and the GitHub
// Environment of the same name holds what the pipeline needs to reach the
// server, written through gh, logged in as the maintainer. The password of
// the environment's Mailpit is chosen once and kept in the .env of the main
// checkout too.
//
// It prints no secret: no token, no private key, no password. The command
// wires its own dependencies by hand, as the commands of the API binary do
// (Commands in CODING_STANDARDS.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/lifecycle"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github/gh"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider/hetzner"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
)

const usage = `Usage: environment <command> <environment>

Commands:
  on        Create the server of the environment, wait until it is ready and
            give its GitHub Environment what reaches it. When it is on
            already, check that it answers, give GitHub its values again and
            create nothing
  off       Destroy the server of the environment and what it had at the
            provider, and take its values out of GitHub, but the password of
            Mailpit. When it is off already, do nothing more
  status    Tell whether the environment is on, since when, at which address
            and host names, and which open pull request has its label
  publish   Publish the images of a commit of a pull request to the server of
            the environment, when the pull request is open and has the label
            of the environment: write its variables file, start its data
            again from the seed, start the API and the worker, wait until /up
            answers, take the label off every other open pull request, with a
            comment there, and comment the addresses on the pull request. The
            pipeline runs it, with these variables: COMMIT and PULL_REQUEST,
            and SERVER_ADDRESS, SERVER_HOST_KEY, DEPLOY_KEY and
            MAILPIT_PASSWORD from the GitHub Environment

The one environment is staging. Its choices are in deploy/server.staging.yaml.
`

// sshRetryInterval is the pause between two attempts to log in to a server
// that is starting.
const sshRetryInterval = 5 * time.Second

// command does the work of one command on an environment and writes what it
// did on the output.
type command func(ctx context.Context, environment environment, stdout io.Writer) error

var commands = map[string]command{
	"on":      turnOn,
	"off":     turnOff,
	"status":  tellStatus,
	"publish": publish,
}

// adapter is how the tool reaches one provider: the variable that holds its
// token, and what connects with it.
type adapter struct {
	tokenVariable string
	connect       func(token config.Secret, labels map[string]string) provider.Provider
}

// adapters maps the provider that the choices of an environment name to its
// adapter.
var adapters = map[string]adapter{
	"hetzner": {
		tokenVariable: "HETZNER_TOKEN",
		connect: func(token config.Secret, labels map[string]string) provider.Provider {
			return hetzner.New(hetzner.Settings{Endpoint: hetzner.Endpoint, Token: token, Labels: labels})
		},
	},
}

// readiness waits until a new server is ready to be used.
type readiness interface {
	Wait(ctx context.Context, target sshclient.Target) error
}

// outside is what the commands reach beyond their arguments and their
// environment variables: the directory they start from, the provider,
// readiness, GitHub, the server a publish works on, its app and the clock.
// main gives the real ones; the tests give fakes.
type outside struct {
	directory string
	// connect reaches the provider an environment names, with its token.
	connect   func(providerName string, token config.Secret, labels map[string]string) provider.Provider
	readiness readiness
	github    github.GitHub
	// reachServer logs in to the server a publish works on. What the
	// server prints goes to the output.
	reachServer func(ctx context.Context, target sshclient.Target, output io.Writer) (remoteServer, error)
	app         appReadiness
	now         func() time.Time
}

func main() {
	shutdown := lifecycle.ShutdownNotice(lifecycle.Signals(), os.Exit)
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "environment: %v\n", err)
		os.Exit(1)
	}
	actual := outside{
		directory: directory,
		connect: func(providerName string, token config.Secret, labels map[string]string) provider.Provider {
			return adapters[providerName].connect(token, labels)
		},
		readiness:   sshclient.Readiness{RetryInterval: sshRetryInterval},
		github:      gh.New(gh.Settings{Executable: gh.Executable, Directory: directory}),
		reachServer: openServer,
		app:         upOverHTTPS{client: &http.Client{Timeout: upAskTimeout}, interval: upRetryInterval},
		now:         time.Now,
	}
	os.Exit(run(shutdown, os.Args[1:], config.Environment(), actual, os.Stdout, os.Stderr))
}

// openServer logs in to a server over SSH.
func openServer(ctx context.Context, target sshclient.Target, output io.Writer) (remoteServer, error) {
	remote, err := sshclient.Open(ctx, target, output)
	if err != nil {
		return nil, err
	}
	return remote, nil
}

// run executes one command and returns the exit status of the process: 0
// when it succeeded or had nothing to do, 1 when it failed and 2 when it was
// not understood. The context is the shutdown notice, which a signal ends:
// a turn-on that it cuts short destroys what it had created.
func run(ctx context.Context, arguments []string, variables map[string]string, outside outside, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("environment", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, usage) }
	err := flags.Parse(arguments)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	if flags.NArg() != 2 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	work, known := commands[flags.Arg(0)]
	if !known {
		fmt.Fprintf(stderr, "environment: unknown command %q\n\n%s", flags.Arg(0), usage)
		return 2
	}

	environment, err := open(flags.Arg(1), variables, outside)
	if errors.Is(err, errUnknownEnvironment) {
		fmt.Fprintf(stderr, "environment: %v\n\n%s", err, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "environment: %v\n", err)
		return 1
	}
	if err := work(ctx, environment, stdout); err != nil {
		fmt.Fprintf(stderr, "environment: %v\n", err)
		return 1
	}
	return 0
}
