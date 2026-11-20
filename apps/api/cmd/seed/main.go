// Command seed fills an empty database with a fictitious consultancy and its
// administrator, so that the app opens on the login screen and a developer
// logs in as that administrator, who is one of the people the local login
// provider knows. It is a binary of its own, built into its own image, which
// development and staging run and production never has (ADR-0019).
//
// It creates its data through the use cases of the setup, as a person would:
// it issues a setup code and concludes the setup with it, so the seeded data
// obeys every rule of the domain and the seed runs no SQL of its own. On a
// database whose setup is concluded it changes nothing and succeeds.
//
// Two runs on an empty database produce the same consultancy, administrator
// and roles, with the same identifiers: each identifier is the next value of
// a sequence, and the seed takes the same steps in the same order. What is
// the moment of the run differs: the times recorded, and the hash of the
// setup code, which is random so that a run cut short leaves no code anyone
// could guess.
//
// The command wires its own dependencies by hand, as the commands of the API
// binary do (Commands in CODING_STANDARDS.md).
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	auditpostgres "github.com/dannilodms/software-factory-manager/apps/api/internal/audit/adapters/postgres"
	auditapplication "github.com/dannilodms/software-factory-manager/apps/api/internal/audit/application"
	consultancypostgres "github.com/dannilodms/software-factory-manager/apps/api/internal/consultancy/adapters/postgres"
	consultancyapplication "github.com/dannilodms/software-factory-manager/apps/api/internal/consultancy/application"
	filespostgres "github.com/dannilodms/software-factory-manager/apps/api/internal/files/adapters/postgres"
	filesapplication "github.com/dannilodms/software-factory-manager/apps/api/internal/files/application"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/clock"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/database"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/lifecycle"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/telemetry"
	"github.com/dannilodms/software-factory-manager/apps/api/migrations"
)

const usage = `Usage: seed

Fills an empty database with a fictitious consultancy and its administrator,
through the use cases of the setup, so that the app opens on the login screen.
On a database whose setup is concluded it changes nothing.

The configuration comes from environment variables: see .env.example.
`

// commandTracer names the tracer that opens the span of the command.
const commandTracer = "github.com/dannilodms/software-factory-manager/apps/api/cmd/seed"

func main() {
	shutdown := lifecycle.ShutdownNotice(lifecycle.Signals(), os.Exit)
	os.Exit(run(shutdown, os.Args[1:], config.Environment(), os.Stdout, os.Stderr))
}

// run executes the seed and returns the exit status of the process: 0 when it
// succeeded or had nothing to do, 1 when it failed and 2 when it was not
// understood. The context is the shutdown notice, which a signal ends.
func run(ctx context.Context, arguments []string, environment map[string]string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, usage) }
	err := flags.Parse(arguments)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "seed: unexpected argument %q\n\n%s", flags.Arg(0), usage)
		return 2
	}

	if err := seed(ctx, environment, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "seed: %v\n", err)
		return 1
	}
	return 0
}

// seed reads the configuration, reaches the database and fills it. Its logs
// are JSON lines on the error output, as the API's are, and hold nothing of
// the data: the output of the command names the consultancy and the
// administrator, and never an e-mail address.
//
// When it returns, the span of the command ends, the pool is closed and then
// the last spans are sent, in that order.
func seed(ctx context.Context, environment map[string]string, stdout, stderr io.Writer) error {
	settings, err := config.Load(config.Seed, environment)
	if err != nil {
		return err
	}

	logger := telemetry.NewLogger(stderr, settings.LogLevel)
	telemetry.RouteTheStandardLogger(logger)
	tracing, pool, stop, err := tracedDatabase(settings, logger)
	if err != nil {
		return err
	}
	defer stop()

	ctx, span := tracing.Provider.Tracer(commandTracer).Start(ctx, "seed")
	defer span.End()
	if err := checkSchema(ctx, pool, settings); err != nil {
		return err
	}

	state, err := consultancyapplication.NewTellSetupState(consultancypostgres.NewSetupStateReader(pool), clock.System{}).Execute(ctx)
	if err != nil {
		return fmt.Errorf("tell the state of the setup: %w", err)
	}
	if state.Concluded {
		fmt.Fprint(stdout, nothingToDoText)
		return nil
	}
	return concludeTheSetup(ctx, pool, settings.SeedMailbox, stdout)
}

// concludeTheSetup goes through the setup as a person would: it issues a
// setup code, which ends any code waiting, and concludes the setup with it.
// The code lives in memory only; a run that fails in between leaves it
// waiting for an hour, as any issued code, and the next run ends it.
func concludeTheSetup(ctx context.Context, pool *pgxpool.Pool, mailbox config.Mailbox, stdout io.Writer) error {
	transactions := database.NewTransactions(pool)
	setupCodes := consultancypostgres.NewSetupCodeRepository(pool)
	consultancies := consultancypostgres.NewConsultancyRepository(pool)
	recordEntry := auditapplication.NewRecordEntry(auditpostgres.NewAuditLog(pool))

	issue := consultancyapplication.NewIssueSetupCode(transactions, setupCodes, consultancies, recordEntry, clock.System{}, rand.Reader)
	issued, err := issue.Execute(ctx)
	if err != nil {
		return fmt.Errorf("issue the setup code: %w", err)
	}

	conclude := consultancyapplication.NewConcludeSetup(consultancyapplication.SetupDependencies{
		Transactions:  transactions,
		SetupCodes:    setupCodes,
		Consultancies: consultancies,
		Users:         consultancypostgres.NewUserRepository(pool),
		Roles:         consultancypostgres.NewRoleRepository(pool),
		// The seed chooses no image, so the confirmation never reads the
		// storage: the seed has none.
		ConfirmUpload: filesapplication.NewConfirmUpload(transactions, filespostgres.NewFileRepository(pool), noFileStorage{}, clock.System{}, rand.Reader),
		RecordEntry:   recordEntry,
		Clock:         clock.System{},
	})
	consultancy := seededConsultancy(mailbox)
	administrator := seededAdministrator(mailbox)
	err = conclude.Execute(ctx, consultancyapplication.ConcludeSetupInput{
		Code:          issued.Code.Reveal(),
		Consultancy:   consultancy,
		Administrator: administrator,
	})
	if err != nil {
		return fmt.Errorf("conclude the setup: %w", err)
	}

	fmt.Fprintf(stdout, seededText, consultancy.Name, administrator.Name)
	return nil
}

const seededText = `Registered the consultancy %s and its administrator, %s.
The app opens on the login screen. The administrator logs in with the address
of SEED_MAILBOX tagged +admin: with a login code, which Mailpit receives, or
through the local login provider, where there is one.
`

const nothingToDoText = `The setup is concluded: the seed changes nothing.
`

// tracedDatabase starts the tracing of the command, under the service "seed",
// and the pool of the database of the API role, whose queries it traces. The
// pool opens no connection yet. stop closes the pool and then sends the last
// spans, in that order: call it once the command is done.
func tracedDatabase(settings config.Config, logger *slog.Logger) (_ *telemetry.Tracing, _ *pgxpool.Pool, stop func(), _ error) {
	tracing, err := telemetry.StartTracing("seed", settings.TelemetryExportURL.String(), logger)
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

// checkSchema stops the command, with what to do, when PostgreSQL does not
// answer or a migration is pending. It waits as long as one statement may
// run: the command runs once, for a person who is waiting.
func checkSchema(ctx context.Context, pool *pgxpool.Pool, settings config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, settings.DatabaseStatementTimeout)
	defer cancel()

	err := database.CheckSchema(ctx, pool, migrations.Files)
	var version database.SchemaVersionError
	if errors.As(err, &version) && version.Applied < version.Expected {
		return fmt.Errorf("%w. Apply the migrations with the migrate command of the API first", err)
	}
	return err
}
