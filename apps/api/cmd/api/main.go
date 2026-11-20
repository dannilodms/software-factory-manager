// Command api is the binary of the backend. With no command it serves the
// API, `migrate` applies the pending migrations, `issue-setup-code` issues
// the setup code that opens the setup, `worker` runs the jobs and
// `probe-readiness` asks the API whether it is ready, for the health check of
// its container.
//
// Each command wires its own dependencies by hand, in its own file: it reads
// the configuration, builds what the command needs and passes each part to
// the ones that use it. Nothing is wired anywhere else, and there is no
// injection library.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/database"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/lifecycle"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/telemetry"
)

const usage = `Usage: api [command]

Commands:
  (none)              Serve the API
  migrate             Apply the pending migrations
  issue-setup-code    Issue the setup code that opens the setup
  worker              Run the jobs
  probe-readiness     Succeed when the API at API_LISTEN_ADDRESS is ready

The configuration comes from environment variables: see .env.example.
`

// errLogged is returned by a command that already wrote its failure to its
// logs, so that run does not print it a second time.
var errLogged = errors.New("the failure is in the logs")

func main() {
	shutdown := lifecycle.ShutdownNotice(lifecycle.Signals(), os.Exit)
	os.Exit(run(shutdown, os.Args[1:], config.Environment(), os.Stdout, os.Stderr))
}

// run executes one command and returns the exit status of the process: 0 when
// the command succeeded, 1 when it failed and 2 when it was not understood.
//
// The context is the shutdown notice: it ends when the process receives
// SIGTERM or SIGINT. A command that runs to its end passes it on, so that a
// signal stops it. The command that serves only watches it.
func run(ctx context.Context, arguments []string, environment map[string]string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("api", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, usage) }
	err := flags.Parse(arguments)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}

	command, known := commands[flags.Arg(0)]
	if !known {
		fmt.Fprintf(stderr, "api: unknown command %q\n\n%s", flags.Arg(0), usage)
		return 2
	}
	err = command(ctx, environment, stdout, stderr)
	if errors.Is(err, errLogged) {
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "api: %v\n", err)
		return 1
	}
	return 0
}

// commands maps the first argument to the command it names. No argument
// serves the API.
var commands = map[string]func(ctx context.Context, environment map[string]string, stdout, stderr io.Writer) error{
	"":                 serveAPI,
	"migrate":          migrate,
	"issue-setup-code": issueSetupCode,
	"worker":           workJobs,
	"probe-readiness":  probeReadiness,
}

// tracedDatabase starts the tracing of a command, under the service "api",
// which every command of this binary uses, and the pool of the database of
// the API role, whose queries it traces. The pool opens no connection yet.
// stop closes the pool and then sends the last spans, in that order: call it
// once the command is done.
func tracedDatabase(settings config.Config, logger *slog.Logger) (_ *telemetry.Tracing, _ *pgxpool.Pool, stop func(), _ error) {
	tracing, err := telemetry.StartTracing("api", settings.TelemetryExportURL.String(), logger)
	if err != nil {
		return nil, nil, nil, err
	}
	pool, err := database.NewPool(database.PoolSettings{
		ConnectionURL:            settings.DatabaseURL,
		MaxSize:                  settings.DatabasePoolMaxSize,
		StatementTimeout:         settings.DatabaseStatementTimeout,
		IdleInTransactionTimeout: settings.DatabaseIdleInTransactionTimeout,
		TracerProvider:           tracing.Provider,
	})
	if err != nil {
		tracing.Stop()
		return nil, nil, nil, err
	}
	stop = func() {
		pool.Close()
		tracing.Stop()
	}
	return tracing, pool, stop, nil
}
