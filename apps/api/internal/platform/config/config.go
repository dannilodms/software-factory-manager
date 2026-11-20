// Package config reads the configuration of the binary from environment
// variables, once, into one typed struct. No other package reads the
// environment. Rules: Configuration in CODING_STANDARDS.md.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Command is a command of the binary. Each one requires its own variables.
type Command string

const (
	// ServeAPI is the command that serves the API.
	ServeAPI Command = "api"
	// Migrate is the command that applies the pending migrations.
	Migrate Command = "migrate"
	// IssueSetupCode is the command that issues a setup code.
	IssueSetupCode Command = "issue-setup-code"
	// Seed is the seed, a binary of its own that fills an empty database.
	Seed Command = "seed"
	// Worker is the command that runs the jobs.
	Worker Command = "worker"
	// ProbeReadiness is the command that asks the API it runs beside whether
	// it is ready: the health check of its container.
	ProbeReadiness Command = "probe-readiness"
)

// Config holds every value the binary takes from the environment. Each
// variable is listed, with what it is for, in .env.example at the root.
type Config struct {
	ListenAddress                    ListenAddress `env:"API_LISTEN_ADDRESS"`
	ShutdownDrainDeadline            time.Duration `env:"SHUTDOWN_DRAIN_DEADLINE" envDefault:"20s"`
	LogLevel                         slog.Level    `env:"LOG_LEVEL" envDefault:"info"`
	TelemetryExportURL               HTTPURL       `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	DatabaseURL                      PostgresURL   `env:"DATABASE_URL"`
	DatabasePoolMaxSize              int32         `env:"DATABASE_POOL_MAX_SIZE" envDefault:"10"`
	DatabaseStatementTimeout         time.Duration `env:"DATABASE_STATEMENT_TIMEOUT" envDefault:"30s"`
	DatabaseIdleInTransactionTimeout time.Duration `env:"DATABASE_IDLE_IN_TRANSACTION_TIMEOUT" envDefault:"1m"`
	MigrationDatabaseURL             PostgresURL   `env:"MIGRATION_DATABASE_URL"`
	HealthCheckTimeout               time.Duration `env:"HEALTH_CHECK_TIMEOUT" envDefault:"2s"`
	AppOrigin                        Origin        `env:"APP_ORIGIN"`
	FileStorageURL                   Origin        `env:"FILE_STORAGE_URL"`
	FileStoragePublicURL             Origin        `env:"FILE_STORAGE_PUBLIC_URL"`
	FileStorageRegion                string        `env:"FILE_STORAGE_REGION"`
	FileStorageBucket                string        `env:"FILE_STORAGE_BUCKET"`
	FileStorageAccessKeyID           string        `env:"FILE_STORAGE_ACCESS_KEY_ID"`
	FileStorageSecretAccessKey       Secret        `env:"FILE_STORAGE_SECRET_ACCESS_KEY"`
	SeedMailbox                      Mailbox       `env:"SEED_MAILBOX"`
	LoginCodeKey                     Key           `env:"LOGIN_CODE_KEY"`
	WorkerLightQueueMaxJobs          int           `env:"WORKER_LIGHT_QUEUE_MAX_JOBS" envDefault:"10"`
	WorkerHeavyQueueMaxJobs          int           `env:"WORKER_HEAVY_QUEUE_MAX_JOBS" envDefault:"2"`
	WorkerShutdownDeadline           time.Duration `env:"WORKER_SHUTDOWN_DEADLINE" envDefault:"20s"`
	EmailSender                      EmailSender   `env:"EMAIL_SENDER"`
	EmailSMTPAddress                 ServerAddress `env:"EMAIL_SMTP_ADDRESS"`
	EmailFromAddress                 EmailAddress  `env:"EMAIL_FROM_ADDRESS"`
	EmailFromName                    string        `env:"EMAIL_FROM_NAME"`
	ResendAPIKey                     Secret        `env:"RESEND_API_KEY"`
	ResendWebhookSecret              WebhookSecret `env:"RESEND_WEBHOOK_SECRET"`
	// The local login provider, on when its variables are set and off when
	// they are empty (localLoginProviderVariables).
	LocalLoginProviderKey          ProviderKey `env:"LOCAL_LOGIN_PROVIDER_KEY"`
	LocalLoginProviderDisplayName  string      `env:"LOCAL_LOGIN_PROVIDER_DISPLAY_NAME"`
	LocalLoginProviderIssuerURL    HTTPURL     `env:"LOCAL_LOGIN_PROVIDER_ISSUER_URL"`
	LocalLoginProviderClientID     string      `env:"LOCAL_LOGIN_PROVIDER_CLIENT_ID"`
	LocalLoginProviderClientSecret Secret      `env:"LOCAL_LOGIN_PROVIDER_CLIENT_SECRET"`
}

// LocalLoginProviderIsOn tells whether the local login provider is turned
// on: its variables are set. Load refuses them half set.
func (c Config) LocalLoginProviderIsOn() bool {
	return c.LocalLoginProviderKey != ""
}

// requiredBy lists the variables with no default that each command needs. The
// API never receives the connection of the migration role, and `migrate`
// never receives the one of the API role.
var requiredBy = map[Command][]string{
	ServeAPI: {
		"API_LISTEN_ADDRESS", "DATABASE_URL",
		"FILE_STORAGE_URL", "FILE_STORAGE_PUBLIC_URL", "FILE_STORAGE_REGION", "FILE_STORAGE_BUCKET",
		"FILE_STORAGE_ACCESS_KEY_ID", "FILE_STORAGE_SECRET_ACCESS_KEY",
		// The cookie of the session goes over HTTPS only when the app does,
		// and a request that changes state with it comes from the app.
		"APP_ORIGIN",
		// The check of a login code computes the code with its key.
		"LOGIN_CODE_KEY",
	},
	Migrate:        {"MIGRATION_DATABASE_URL"},
	IssueSetupCode: {"DATABASE_URL", "APP_ORIGIN"},
	Seed:           {"DATABASE_URL", "SEED_MAILBOX"},
	// The worker delivers the e-mails, through the sender EMAIL_SENDER names,
	// which needs variables of its own (requiredBySender), and cleans up the
	// file storage.
	Worker: {
		"DATABASE_URL", "EMAIL_SENDER", "EMAIL_FROM_ADDRESS", "EMAIL_FROM_NAME",
		"FILE_STORAGE_URL", "FILE_STORAGE_PUBLIC_URL", "FILE_STORAGE_REGION", "FILE_STORAGE_BUCKET",
		"FILE_STORAGE_ACCESS_KEY_ID", "FILE_STORAGE_SECRET_ACCESS_KEY",
		// The delivery of a login code computes the code with its key.
		"LOGIN_CODE_KEY",
	},
	// The probe reaches the API where the API listens.
	ProbeReadiness: {"API_LISTEN_ADDRESS"},
}

// requiredBySender lists the variables each sender of e-mail needs, for a
// command that requires EMAIL_SENDER: the SMTP server, or the key of Resend.
var requiredBySender = map[EmailSender][]string{
	SMTPSender:   {"EMAIL_SMTP_ADDRESS"},
	ResendSender: {"RESEND_API_KEY"},
}

// localLoginProviderVariables turn the local login provider on, all of them
// together, for the API, which logs in through it.
var localLoginProviderVariables = []string{
	"LOCAL_LOGIN_PROVIDER_KEY", "LOCAL_LOGIN_PROVIDER_DISPLAY_NAME", "LOCAL_LOGIN_PROVIDER_ISSUER_URL",
	"LOCAL_LOGIN_PROVIDER_CLIENT_ID", "LOCAL_LOGIN_PROVIDER_CLIENT_SECRET",
}

// requiredForLocalLoginProvider gives the variables of the local login
// provider when the API is told to turn it on: one of them is set.
func requiredForLocalLoginProvider(command Command, environment map[string]string) []string {
	if command != ServeAPI {
		return nil
	}
	set := slices.ContainsFunc(localLoginProviderVariables, func(variable string) bool { return environment[variable] != "" })
	if !set {
		return nil
	}
	return localLoginProviderVariables
}

// requiredForEmail gives the variables the sender of e-mail the environment
// names needs, when the command sends e-mail.
func requiredForEmail(command Command, environment map[string]string) []string {
	if !slices.Contains(requiredBy[command], "EMAIL_SENDER") {
		return nil
	}
	return requiredBySender[EmailSender(environment["EMAIL_SENDER"])]
}

// Load reads the environment into a Config for one command. A variable that
// is missing or not valid is an error whose text names the variable and never
// its value, so the text is safe to print.
func Load(command Command, environment map[string]string) (Config, error) {
	var loaded Config
	found := problems{}

	required := slices.Concat(requiredBy[command], requiredForEmail(command, environment), requiredForLocalLoginProvider(command, environment))
	for _, variable := range required {
		found.addWhen(environment[variable] == "", variable, "is required")
	}
	found.addUnparsed(env.ParseWithOptions(&loaded, env.Options{Environment: environment}))
	found.addWhen(loaded.ShutdownDrainDeadline <= 0, "SHUTDOWN_DRAIN_DEADLINE", "is not valid: expected more than zero")
	found.addWhen(loaded.DatabasePoolMaxSize < 1, "DATABASE_POOL_MAX_SIZE", "is not valid: expected at least 1")
	found.addWhen(loaded.DatabaseStatementTimeout <= 0, "DATABASE_STATEMENT_TIMEOUT", "is not valid: expected more than zero")
	found.addWhen(loaded.DatabaseIdleInTransactionTimeout <= 0, "DATABASE_IDLE_IN_TRANSACTION_TIMEOUT", "is not valid: expected more than zero")
	found.addWhen(loaded.HealthCheckTimeout <= 0, "HEALTH_CHECK_TIMEOUT", "is not valid: expected more than zero")
	found.addWhen(loaded.WorkerLightQueueMaxJobs < 1, "WORKER_LIGHT_QUEUE_MAX_JOBS", "is not valid: expected at least 1")
	found.addWhen(loaded.WorkerHeavyQueueMaxJobs < 1, "WORKER_HEAVY_QUEUE_MAX_JOBS", "is not valid: expected at least 1")
	found.addWhen(loaded.WorkerShutdownDeadline <= 0, "WORKER_SHUTDOWN_DEADLINE", "is not valid: expected more than zero")

	if len(found) > 0 {
		return Config{}, errors.New("configuration: " + found.line())
	}
	return loaded, nil
}

// problems maps a variable to what is wrong with it. The first problem found
// for a variable is the one kept.
type problems map[string]string

func (p problems) addWhen(broken bool, variable, problem string) {
	if !broken {
		return
	}
	if _, known := p[variable]; known {
		return
	}
	p[variable] = problem
}

// addUnparsed records the values the library could not read. Its own messages
// quote the value, so each one is replaced by what was expected instead.
func (p problems) addUnparsed(err error) {
	var aggregate env.AggregateError
	if !errors.As(err, &aggregate) {
		p.addWhen(err != nil, "the environment", "could not be read")
		return
	}
	for _, each := range aggregate.Errors {
		p.addOneUnparsed(each)
	}
}

func (p problems) addOneUnparsed(err error) {
	var unparsed env.ParseError
	if !errors.As(err, &unparsed) {
		p.addWhen(true, "the environment", "could not be read")
		return
	}
	p.addWhen(true, variableOf(unparsed.Name), "is not valid: expected "+expectationOf(unparsed.Type))
}

// line lists the problems in the order the variables are declared in Config.
func (p problems) line() string {
	variables := make([]string, 0, len(p))
	for variable := range p {
		variables = append(variables, variable)
	}
	declared := declaredVariables()
	slices.SortFunc(variables, func(a, b string) int {
		return slices.Index(declared, a) - slices.Index(declared, b)
	})

	parts := make([]string, 0, len(variables))
	for _, variable := range variables {
		parts = append(parts, variable+" "+p[variable])
	}
	return strings.Join(parts, "; ")
}

func declaredVariables() []string {
	fields := reflect.VisibleFields(reflect.TypeFor[Config]())
	variables := make([]string, 0, len(fields))
	for _, field := range fields {
		variables = append(variables, field.Tag.Get("env"))
	}
	return variables
}

func variableOf(fieldName string) string {
	field, found := reflect.TypeFor[Config]().FieldByName(fieldName)
	if !found {
		return "the environment"
	}
	return field.Tag.Get("env")
}

func expectationOf(valueType reflect.Type) string {
	switch valueType {
	case reflect.TypeFor[slog.Level]():
		return "debug, info, warn or error"
	case reflect.TypeFor[HTTPURL]():
		return "an http or https URL"
	case reflect.TypeFor[ListenAddress]():
		return "host:port"
	case reflect.TypeFor[PostgresURL]():
		return "a PostgreSQL connection URL"
	case reflect.TypeFor[Origin]():
		return "an origin such as https://example.com"
	case reflect.TypeFor[Mailbox]():
		return "a mailbox such as name@example.com"
	case reflect.TypeFor[EmailAddress]():
		return "an e-mail address such as name@example.com"
	case reflect.TypeFor[EmailSender]():
		return "smtp or resend"
	case reflect.TypeFor[WebhookSecret]():
		return "whsec_ followed by the key in base64"
	case reflect.TypeFor[ServerAddress]():
		return "host:port, with a host"
	case reflect.TypeFor[ProviderKey]():
		return "lowercase letters, digits and underscores, starting with a letter, 64 at most, other than the way in of the login code"
	case reflect.TypeFor[Key]():
		return fmt.Sprintf("at least %d hexadecimal digits (%d bytes)", 2*KeyMinSize, KeyMinSize)
	case reflect.TypeFor[time.Duration]():
		return "a duration such as 30s"
	case reflect.TypeFor[int32](), reflect.TypeFor[int]():
		return "a whole number"
	}
	return "another kind of value"
}
