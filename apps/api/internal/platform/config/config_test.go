package config_test

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
)

func validEnvironment() map[string]string {
	return map[string]string{
		"API_LISTEN_ADDRESS":             "127.0.0.1:8080",
		"DATABASE_URL":                   "postgres://api:api-password@localhost:5432/software_factory",
		"MIGRATION_DATABASE_URL":         "postgres://migration:migration-password@localhost:5432/software_factory",
		"APP_ORIGIN":                     "https://factory.example.com",
		"FILE_STORAGE_URL":               "http://garage:3900",
		"FILE_STORAGE_PUBLIC_URL":        "https://files.factory.example.com",
		"FILE_STORAGE_REGION":            "garage",
		"FILE_STORAGE_BUCKET":            "factory-files",
		"FILE_STORAGE_ACCESS_KEY_ID":     "GK0123456789abcdef01234567",
		"FILE_STORAGE_SECRET_ACCESS_KEY": "storage-secret-access-key",
		"SEED_MAILBOX":                   "seed@fabrica.example",
		"EMAIL_SENDER":                   "smtp",
		"EMAIL_SMTP_ADDRESS":             "mailpit:1025",
		"EMAIL_FROM_ADDRESS":             "no-reply@fabrica.example",
		"EMAIL_FROM_NAME":                "Fábrica Exemplo",
		"LOGIN_CODE_KEY":                 "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
	}
}

// workerEnvironment is what the worker needs, and nothing else.
func workerEnvironment() map[string]string {
	return map[string]string{
		"DATABASE_URL":       "postgres://api:api-password@localhost:5432/software_factory",
		"EMAIL_SENDER":       "smtp",
		"EMAIL_SMTP_ADDRESS": "mailpit:1025",
		"EMAIL_FROM_ADDRESS": "no-reply@fabrica.example",
		"EMAIL_FROM_NAME":    "Fábrica Exemplo",
		// The worker cleans up the file storage.
		"FILE_STORAGE_URL":               "http://localhost:3900",
		"FILE_STORAGE_PUBLIC_URL":        "http://files.localhost",
		"FILE_STORAGE_REGION":            "garage",
		"FILE_STORAGE_BUCKET":            "software-factory-files",
		"FILE_STORAGE_ACCESS_KEY_ID":     "GK0123",
		"FILE_STORAGE_SECRET_ACCESS_KEY": "a-secret-key",
		// The delivery of a login code computes the code with its key.
		"LOGIN_CODE_KEY": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
	}
}

func TestTheMailboxOfTheSeedGivesTheAddressOfEachSeededUser(t *testing.T) {
	loaded, err := config.Load(config.Seed, validEnvironment())

	require.NoError(t, err)
	assert.Equal(t, "seed@fabrica.example", loaded.SeedMailbox.String())
	assert.Equal(t, "seed+admin@fabrica.example", loaded.SeedMailbox.Subaddress("admin"))
	assert.Equal(t, "seed+developer@fabrica.example", loaded.SeedMailbox.Subaddress("developer"))
}

func TestTheSeedNeedsOnlyTheDatabaseAndTheMailbox(t *testing.T) {
	_, err := config.Load(config.Seed, map[string]string{
		"DATABASE_URL": "postgres://api:api-password@localhost:5432/software_factory",
		"SEED_MAILBOX": "seed@fabrica.example",
	})

	require.NoError(t, err)
}

func TestLoadReadsWhereTheFileStorageIsAndTheKeyToSignWith(t *testing.T) {
	loaded, err := config.Load(config.ServeAPI, validEnvironment())

	require.NoError(t, err)
	assert.Equal(t, "http://garage:3900", loaded.FileStorageURL.String())
	assert.Equal(t, "https://files.factory.example.com", loaded.FileStoragePublicURL.String())
	assert.Equal(t, "garage", loaded.FileStorageRegion)
	assert.Equal(t, "factory-files", loaded.FileStorageBucket)
	assert.Equal(t, "GK0123456789abcdef01234567", loaded.FileStorageAccessKeyID)
	assert.Equal(t, "storage-secret-access-key", loaded.FileStorageSecretAccessKey.Reveal())
}

func TestALoadedConfigurationNeverPrintsTheSecretKeyOfTheFileStorage(t *testing.T) {
	loaded, err := config.Load(config.ServeAPI, validEnvironment())
	require.NoError(t, err)

	for _, printed := range []string{
		fmt.Sprint(loaded), fmt.Sprintf("%+v", loaded), fmt.Sprintf("%#v", loaded), loaded.FileStorageSecretAccessKey.String(),
	} {
		assert.NotContains(t, printed, "storage-secret-access-key")
	}
}

func TestOnlyTheAPINeedsTheFileStorage(t *testing.T) {
	environment := validEnvironment()
	maps.DeleteFunc(environment, func(variable, _ string) bool { return strings.HasPrefix(variable, "FILE_STORAGE_") })

	_, migrate := config.Load(config.Migrate, environment)
	_, issue := config.Load(config.IssueSetupCode, environment)

	assert.NoError(t, migrate)
	assert.NoError(t, issue)
}

func without(environment map[string]string, variable string) map[string]string {
	delete(environment, variable)
	return environment
}

func with(environment map[string]string, variable, value string) map[string]string {
	environment[variable] = value
	return environment
}

func TestLoadReadsTheEnvironmentIntoTypedValues(t *testing.T) {
	environment := validEnvironment()
	environment["SHUTDOWN_DRAIN_DEADLINE"] = "5s"
	environment["LOG_LEVEL"] = "debug"
	environment["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://collector:4318"
	environment["DATABASE_POOL_MAX_SIZE"] = "4"
	environment["DATABASE_STATEMENT_TIMEOUT"] = "5s"
	environment["DATABASE_IDLE_IN_TRANSACTION_TIMEOUT"] = "2m"
	environment["HEALTH_CHECK_TIMEOUT"] = "500ms"

	loaded, err := config.Load(config.ServeAPI, environment)

	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:8080", loaded.ListenAddress.String())
	assert.Equal(t, 5*time.Second, loaded.ShutdownDrainDeadline)
	assert.Equal(t, slog.LevelDebug, loaded.LogLevel)
	assert.Equal(t, "http://collector:4318", loaded.TelemetryExportURL.String())
	assert.Equal(t, "postgres://api:api-password@localhost:5432/software_factory", loaded.DatabaseURL.ConnectionString())
	assert.Equal(t, int32(4), loaded.DatabasePoolMaxSize)
	assert.Equal(t, 5*time.Second, loaded.DatabaseStatementTimeout)
	assert.Equal(t, 2*time.Minute, loaded.DatabaseIdleInTransactionTimeout)
	assert.Equal(t, 500*time.Millisecond, loaded.HealthCheckTimeout)
}

func TestLoadFallsBackToTheDefaults(t *testing.T) {
	loaded, err := config.Load(config.ServeAPI, validEnvironment())

	require.NoError(t, err)
	assert.Equal(t, 20*time.Second, loaded.ShutdownDrainDeadline)
	assert.Equal(t, slog.LevelInfo, loaded.LogLevel)
	assert.False(t, loaded.TelemetryExportURL.IsSet(), "telemetry is exported to no address by default")
	assert.Equal(t, int32(10), loaded.DatabasePoolMaxSize)
	assert.Equal(t, 30*time.Second, loaded.DatabaseStatementTimeout)
	assert.Equal(t, time.Minute, loaded.DatabaseIdleInTransactionTimeout)
	assert.Equal(t, 2*time.Second, loaded.HealthCheckTimeout)
}

func TestAnEmptyExportAddressIsNoAddress(t *testing.T) {
	loaded, err := config.Load(config.ServeAPI, with(validEnvironment(), "OTEL_EXPORTER_OTLP_ENDPOINT", ""))

	require.NoError(t, err)
	assert.False(t, loaded.TelemetryExportURL.IsSet())
}

func TestLoadNamesARequiredVariableThatIsMissing(t *testing.T) {
	cases := []struct {
		command  config.Command
		variable string
	}{
		{config.ServeAPI, "API_LISTEN_ADDRESS"},
		{config.ServeAPI, "DATABASE_URL"},
		{config.ServeAPI, "FILE_STORAGE_URL"},
		{config.ServeAPI, "FILE_STORAGE_PUBLIC_URL"},
		{config.ServeAPI, "FILE_STORAGE_REGION"},
		{config.ServeAPI, "FILE_STORAGE_BUCKET"},
		{config.ServeAPI, "FILE_STORAGE_ACCESS_KEY_ID"},
		{config.ServeAPI, "FILE_STORAGE_SECRET_ACCESS_KEY"},
		{config.ServeAPI, "APP_ORIGIN"},
		{config.ServeAPI, "LOGIN_CODE_KEY"},
		{config.Migrate, "MIGRATION_DATABASE_URL"},
		{config.IssueSetupCode, "DATABASE_URL"},
		{config.IssueSetupCode, "APP_ORIGIN"},
		{config.Seed, "DATABASE_URL"},
		{config.Seed, "SEED_MAILBOX"},
		{config.Worker, "DATABASE_URL"},
		{config.Worker, "EMAIL_SENDER"},
		{config.Worker, "EMAIL_SMTP_ADDRESS"},
		{config.Worker, "EMAIL_FROM_ADDRESS"},
		{config.Worker, "EMAIL_FROM_NAME"},
		{config.Worker, "FILE_STORAGE_URL"},
		{config.Worker, "FILE_STORAGE_PUBLIC_URL"},
		{config.Worker, "FILE_STORAGE_REGION"},
		{config.Worker, "FILE_STORAGE_BUCKET"},
		{config.Worker, "FILE_STORAGE_ACCESS_KEY_ID"},
		{config.Worker, "FILE_STORAGE_SECRET_ACCESS_KEY"},
		{config.Worker, "LOGIN_CODE_KEY"},
		{config.ProbeReadiness, "API_LISTEN_ADDRESS"},
	}
	for _, c := range cases {
		t.Run(string(c.command)+" without "+c.variable, func(t *testing.T) {
			_, err := config.Load(c.command, without(validEnvironment(), c.variable))

			require.EqualError(t, err, "configuration: "+c.variable+" is required")
		})
	}
}

func TestLoadTreatsAnEmptyRequiredVariableAsMissing(t *testing.T) {
	_, err := config.Load(config.ServeAPI, with(validEnvironment(), "DATABASE_URL", ""))

	require.EqualError(t, err, "configuration: DATABASE_URL is required")
}

func TestEachCommandRequiresOnlyTheDatabaseRoleItUses(t *testing.T) {
	_, err := config.Load(config.ServeAPI, without(validEnvironment(), "MIGRATION_DATABASE_URL"))
	require.NoError(t, err)

	_, err = config.Load(config.Migrate, map[string]string{
		"MIGRATION_DATABASE_URL": "postgres://migration:migration-password@localhost:5432/software_factory",
	})
	require.NoError(t, err)
}

func TestLoadNamesAnInvalidVariableAndNeverShowsItsValue(t *testing.T) {
	cases := []struct {
		variable string
		value    string
		line     string
	}{
		{"API_LISTEN_ADDRESS", "no-port-here", "configuration: API_LISTEN_ADDRESS is not valid: expected host:port"},
		{"SHUTDOWN_DRAIN_DEADLINE", "twenty", "configuration: SHUTDOWN_DRAIN_DEADLINE is not valid: expected a duration such as 30s"},
		{"SHUTDOWN_DRAIN_DEADLINE", "0s", "configuration: SHUTDOWN_DRAIN_DEADLINE is not valid: expected more than zero"},
		{"LOG_LEVEL", "chatty", "configuration: LOG_LEVEL is not valid: expected debug, info, warn or error"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4318", "configuration: OTEL_EXPORTER_OTLP_ENDPOINT is not valid: expected an http or https URL"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "grpc://collector:4317", "configuration: OTEL_EXPORTER_OTLP_ENDPOINT is not valid: expected an http or https URL"},
		{"DATABASE_URL", "mysql://api:hunter2@localhost/db", "configuration: DATABASE_URL is not valid: expected a PostgreSQL connection URL"},
		{"DATABASE_URL", "postgres://api:hunter2@localhost:port/db", "configuration: DATABASE_URL is not valid: expected a PostgreSQL connection URL"},
		{"DATABASE_URL", "host=localhost password=hunter2", "configuration: DATABASE_URL is not valid: expected a PostgreSQL connection URL"},
		{"DATABASE_POOL_MAX_SIZE", "plenty", "configuration: DATABASE_POOL_MAX_SIZE is not valid: expected a whole number"},
		{"DATABASE_POOL_MAX_SIZE", "0", "configuration: DATABASE_POOL_MAX_SIZE is not valid: expected at least 1"},
		{"DATABASE_STATEMENT_TIMEOUT", "45", "configuration: DATABASE_STATEMENT_TIMEOUT is not valid: expected a duration such as 30s"},
		{"DATABASE_STATEMENT_TIMEOUT", "-5s", "configuration: DATABASE_STATEMENT_TIMEOUT is not valid: expected more than zero"},
		{"DATABASE_IDLE_IN_TRANSACTION_TIMEOUT", "0s", "configuration: DATABASE_IDLE_IN_TRANSACTION_TIMEOUT is not valid: expected more than zero"},
		{"HEALTH_CHECK_TIMEOUT", "soon", "configuration: HEALTH_CHECK_TIMEOUT is not valid: expected a duration such as 30s"},
		{"HEALTH_CHECK_TIMEOUT", "0s", "configuration: HEALTH_CHECK_TIMEOUT is not valid: expected more than zero"},
		{"APP_ORIGIN", "factory.example.com", "configuration: APP_ORIGIN is not valid: expected an origin such as https://example.com"},
		{"APP_ORIGIN", "ftp://factory.example.com", "configuration: APP_ORIGIN is not valid: expected an origin such as https://example.com"},
		{"APP_ORIGIN", "https://factory.example.com/app", "configuration: APP_ORIGIN is not valid: expected an origin such as https://example.com"},
		{"APP_ORIGIN", "https://factory.example.com?next=setup", "configuration: APP_ORIGIN is not valid: expected an origin such as https://example.com"},
		{"APP_ORIGIN", "https://someone:hunter2@factory.example.com", "configuration: APP_ORIGIN is not valid: expected an origin such as https://example.com"},
		{"FILE_STORAGE_URL", "garage:3900", "configuration: FILE_STORAGE_URL is not valid: expected an origin such as https://example.com"},
		{"FILE_STORAGE_URL", "http://garage:3900/bucket", "configuration: FILE_STORAGE_URL is not valid: expected an origin such as https://example.com"},
		{"FILE_STORAGE_PUBLIC_URL", "files.localhost", "configuration: FILE_STORAGE_PUBLIC_URL is not valid: expected an origin such as https://example.com"},
		{"SEED_MAILBOX", "seed.fabrica.example", "configuration: SEED_MAILBOX is not valid: expected a mailbox such as name@example.com"},
		{"SEED_MAILBOX", "Seed <seed@fabrica.example>", "configuration: SEED_MAILBOX is not valid: expected a mailbox such as name@example.com"},
		{"SEED_MAILBOX", "seed@fabrica.example, other@fabrica.example", "configuration: SEED_MAILBOX is not valid: expected a mailbox such as name@example.com"},
		{"SEED_MAILBOX", "@fabrica.example", "configuration: SEED_MAILBOX is not valid: expected a mailbox such as name@example.com"},
		{"WORKER_LIGHT_QUEUE_MAX_JOBS", "many", "configuration: WORKER_LIGHT_QUEUE_MAX_JOBS is not valid: expected a whole number"},
		{"WORKER_LIGHT_QUEUE_MAX_JOBS", "0", "configuration: WORKER_LIGHT_QUEUE_MAX_JOBS is not valid: expected at least 1"},
		{"WORKER_HEAVY_QUEUE_MAX_JOBS", "-1", "configuration: WORKER_HEAVY_QUEUE_MAX_JOBS is not valid: expected at least 1"},
		{"WORKER_SHUTDOWN_DEADLINE", "soon", "configuration: WORKER_SHUTDOWN_DEADLINE is not valid: expected a duration such as 30s"},
		{"WORKER_SHUTDOWN_DEADLINE", "0s", "configuration: WORKER_SHUTDOWN_DEADLINE is not valid: expected more than zero"},
		{"EMAIL_SENDER", "postfix", "configuration: EMAIL_SENDER is not valid: expected smtp or resend"},
		{"EMAIL_SMTP_ADDRESS", "mailpit", "configuration: EMAIL_SMTP_ADDRESS is not valid: expected host:port, with a host"},
		{"EMAIL_SMTP_ADDRESS", ":1025", "configuration: EMAIL_SMTP_ADDRESS is not valid: expected host:port, with a host"},
		{"EMAIL_SMTP_ADDRESS", "mailpit:0", "configuration: EMAIL_SMTP_ADDRESS is not valid: expected host:port, with a host"},
		{"EMAIL_SMTP_ADDRESS", "mailpit:smtp", "configuration: EMAIL_SMTP_ADDRESS is not valid: expected host:port, with a host"},
		{"EMAIL_FROM_ADDRESS", "no-reply.fabrica.example", "configuration: EMAIL_FROM_ADDRESS is not valid: expected an e-mail address such as name@example.com"},
		{"EMAIL_FROM_ADDRESS", "Fabrica <no-reply@fabrica.example>", "configuration: EMAIL_FROM_ADDRESS is not valid: expected an e-mail address such as name@example.com"},
		{"RESEND_WEBHOOK_SECRET", "c2lnbmluZy1rZXk=", "configuration: RESEND_WEBHOOK_SECRET is not valid: expected whsec_ followed by the key in base64"},
		{"RESEND_WEBHOOK_SECRET", "whsec_not base64!", "configuration: RESEND_WEBHOOK_SECRET is not valid: expected whsec_ followed by the key in base64"},
		{"LOGIN_CODE_KEY", "not-hexadecimal-digits-at-all-not-hexadecimal-digits-at-all-0000", "configuration: LOGIN_CODE_KEY is not valid: expected at least 64 hexadecimal digits (32 bytes)"},
		{"LOGIN_CODE_KEY", "00010203040506070809", "configuration: LOGIN_CODE_KEY is not valid: expected at least 64 hexadecimal digits (32 bytes)"},
	}
	for _, c := range cases {
		t.Run(c.variable+"="+c.value, func(t *testing.T) {
			_, err := config.Load(config.ServeAPI, with(validEnvironment(), c.variable, c.value))

			require.EqualError(t, err, c.line)
			assert.NotContains(t, err.Error(), c.value)
		})
	}
}

func TestLoadReportsEveryProblemInOneLine(t *testing.T) {
	environment := without(validEnvironment(), "API_LISTEN_ADDRESS")
	environment["DATABASE_POOL_MAX_SIZE"] = "plenty"

	_, err := config.Load(config.ServeAPI, environment)

	require.EqualError(t, err, "configuration: API_LISTEN_ADDRESS is required; DATABASE_POOL_MAX_SIZE is not valid: expected a whole number")
}

func TestAnInvalidValueOfAVariableTheCommandDoesNotUseStillStopsIt(t *testing.T) {
	_, err := config.Load(config.Migrate, map[string]string{
		"MIGRATION_DATABASE_URL": "postgres://migration:migration-password@localhost:5432/software_factory",
		"DATABASE_POOL_MAX_SIZE": "plenty",
	})

	require.EqualError(t, err, "configuration: DATABASE_POOL_MAX_SIZE is not valid: expected a whole number")
}

func TestALoadedConfigurationNeverPrintsAPassword(t *testing.T) {
	loaded, err := config.Load(config.ServeAPI, validEnvironment())
	require.NoError(t, err)

	for _, printed := range []string{fmt.Sprint(loaded), fmt.Sprintf("%+v", loaded), fmt.Sprintf("%#v", loaded.DatabaseURL.String())} {
		assert.NotContains(t, printed, "api-password")
		assert.NotContains(t, printed, "migration-password")
	}
}

func TestTheOriginOfTheAppIsReadWithoutATrailingSlash(t *testing.T) {
	for _, value := range []string{"https://factory.example.com", "https://factory.example.com/", "http://localhost:5173"} {
		loaded, err := config.Load(config.IssueSetupCode, with(validEnvironment(), "APP_ORIGIN", value))

		require.NoError(t, err)
		assert.Equal(t, strings.TrimSuffix(value, "/"), loaded.AppOrigin.String())
	}
}

// The origin of the app is read as a browser writes the Origin of a request,
// so the check of the origin compares like with like: the scheme and the host
// in lowercase, and no port that is the default of the scheme.
func TestTheOriginOfTheAppIsReadAsABrowserWritesIt(t *testing.T) {
	for value, read := range map[string]string{
		"HTTPS://Factory.Example.COM":      "https://factory.example.com",
		"https://factory.example.com:443":  "https://factory.example.com",
		"http://localhost:80/":             "http://localhost",
		"https://factory.example.com:8443": "https://factory.example.com:8443",
		"http://LOCALHOST:5173":            "http://localhost:5173",
		"http://[::1]:80":                  "http://[::1]",
		"http://[::1]:5173":                "http://[::1]:5173",
	} {
		loaded, err := config.Load(config.ServeAPI, with(validEnvironment(), "APP_ORIGIN", value))

		require.NoError(t, err, value)
		assert.Equal(t, read, loaded.AppOrigin.String(), value)
	}
}

func TestAnOriginIsReadFromAnyTextAsAnOriginOfTheAppIs(t *testing.T) {
	read, err := config.NewOrigin("https://Factory.example.com:443")
	require.NoError(t, err)
	assert.Equal(t, "https://factory.example.com", read.String())

	for _, text := range []string{"null", "", "https://factory.example.com/app", "factory.example.com"} {
		_, err := config.NewOrigin(text)
		assert.Error(t, err, text)
	}
}

func TestTheOriginOfTheAppTellsWhetherItIsServedOverHTTPS(t *testing.T) {
	for value, overHTTPS := range map[string]bool{"https://factory.example.com": true, "http://localhost": false} {
		loaded, err := config.Load(config.ServeAPI, with(validEnvironment(), "APP_ORIGIN", value))

		require.NoError(t, err)
		assert.Equal(t, overHTTPS, loaded.AppOrigin.IsHTTPS(), value)
	}
}

func TestIssueSetupCodeNeedsNeitherTheListenAddressNorTheMigrationRole(t *testing.T) {
	_, err := config.Load(config.IssueSetupCode, map[string]string{
		"DATABASE_URL": "postgres://api:api-password@localhost:5432/software_factory",
		"APP_ORIGIN":   "https://factory.example.com",
	})

	require.NoError(t, err)
}

func TestTheWorkerNeedsTheDatabaseTheSenderOfEmailAndTheFileStorageAndNothingOfTheListeningOfTheAPI(t *testing.T) {
	loaded, err := config.Load(config.Worker, workerEnvironment())

	require.NoError(t, err)
	assert.Equal(t, 10, loaded.WorkerLightQueueMaxJobs)
	assert.Equal(t, 2, loaded.WorkerHeavyQueueMaxJobs)
	assert.Equal(t, 20*time.Second, loaded.WorkerShutdownDeadline)
}

func TestLoadReadsTheSenderOfEmail(t *testing.T) {
	loaded, err := config.Load(config.Worker, workerEnvironment())

	require.NoError(t, err)
	assert.Equal(t, config.SMTPSender, loaded.EmailSender)
	assert.Equal(t, "mailpit", loaded.EmailSMTPAddress.Host())
	assert.Equal(t, 1025, loaded.EmailSMTPAddress.Port())
	assert.Equal(t, "mailpit:1025", loaded.EmailSMTPAddress.String())
	assert.Equal(t, "no-reply@fabrica.example", loaded.EmailFromAddress.String())
	assert.Equal(t, "Fábrica Exemplo", loaded.EmailFromName)
}

func TestOnlyTheWorkerNeedsTheSenderOfEmail(t *testing.T) {
	environment := validEnvironment()
	maps.DeleteFunc(environment, func(variable, _ string) bool { return strings.HasPrefix(variable, "EMAIL_") })

	_, serve := config.Load(config.ServeAPI, environment)
	_, migrate := config.Load(config.Migrate, environment)
	_, issue := config.Load(config.IssueSetupCode, environment)
	_, seed := config.Load(config.Seed, environment)

	assert.NoError(t, serve)
	assert.NoError(t, migrate)
	assert.NoError(t, issue)
	assert.NoError(t, seed)
}

func TestLoadReadsTheVariablesOfTheWorker(t *testing.T) {
	environment := validEnvironment()
	environment["WORKER_LIGHT_QUEUE_MAX_JOBS"] = "4"
	environment["WORKER_HEAVY_QUEUE_MAX_JOBS"] = "1"
	environment["WORKER_SHUTDOWN_DEADLINE"] = "45s"

	loaded, err := config.Load(config.Worker, environment)

	require.NoError(t, err)
	assert.Equal(t, 4, loaded.WorkerLightQueueMaxJobs)
	assert.Equal(t, 1, loaded.WorkerHeavyQueueMaxJobs)
	assert.Equal(t, 45*time.Second, loaded.WorkerShutdownDeadline)
}

func TestLoadReadsTheKeyOfTheLoginCodeAndNeverPrintsIt(t *testing.T) {
	loaded, err := config.Load(config.Worker, workerEnvironment())
	require.NoError(t, err)

	key := loaded.LoginCodeKey.Reveal()

	assert.Len(t, key, 32)
	assert.Equal(t, byte(0x1f), key[31])
	for _, printed := range []string{fmt.Sprint(loaded), fmt.Sprintf("%+v", loaded), fmt.Sprintf("%#v", loaded.LoginCodeKey)} {
		assert.NotContains(t, printed, "0a0b0c0d")
	}
}

// The cookie of the session goes over HTTPS only when the app does: the API
// reads where the app is served.
func TestTheAPINeedsTheOriginOfTheApp(t *testing.T) {
	_, err := config.Load(config.ServeAPI, without(validEnvironment(), "APP_ORIGIN"))

	require.EqualError(t, err, "configuration: APP_ORIGIN is required")
}

// The configuration is the one package that reads the environment: the
// command that starts and the support of the tests ask it.
func TestTheEnvironmentIsTheVariablesOfTheProcess(t *testing.T) {
	t.Setenv("CONFIG_TEST_VARIABLE", "a value=with an equals sign")

	read := config.Environment()

	assert.Equal(t, "a value=with an equals sign", read["CONFIG_TEST_VARIABLE"])
}

// The sender of e-mail chooses the variables the worker needs besides the
// sender's address and name: the SMTP server, or the key of Resend.

// resendWorkerEnvironment is what the worker needs to send through Resend,
// and nothing else: no SMTP server.
func resendWorkerEnvironment() map[string]string {
	environment := without(workerEnvironment(), "EMAIL_SMTP_ADDRESS")
	environment["EMAIL_SENDER"] = "resend"
	environment["RESEND_API_KEY"] = "re_api_key_of_the_tests"
	return environment
}

func TestTheWorkerSendsThroughResendWithItsKeyAndNoSMTPServer(t *testing.T) {
	loaded, err := config.Load(config.Worker, resendWorkerEnvironment())

	require.NoError(t, err)
	assert.Equal(t, config.ResendSender, loaded.EmailSender)
	assert.Equal(t, "re_api_key_of_the_tests", loaded.ResendAPIKey.Reveal())
	assert.Equal(t, "no-reply@fabrica.example", loaded.EmailFromAddress.String())
}

func TestTheWorkerNeedsTheKeyOfResendOnlyWhenItSendsThroughResend(t *testing.T) {
	_, throughResend := config.Load(config.Worker, without(resendWorkerEnvironment(), "RESEND_API_KEY"))
	_, throughSMTP := config.Load(config.Worker, workerEnvironment())

	require.EqualError(t, throughResend, "configuration: RESEND_API_KEY is required")
	assert.NoError(t, throughSMTP)
}

func TestTheWorkerNeedsTheSMTPServerOnlyWhenItSendsThroughSMTP(t *testing.T) {
	_, throughSMTP := config.Load(config.Worker, without(workerEnvironment(), "EMAIL_SMTP_ADDRESS"))

	require.EqualError(t, throughSMTP, "configuration: EMAIL_SMTP_ADDRESS is required")
}

func TestAWorkerWithNoSenderOfEmailIsToldToChooseOne(t *testing.T) {
	_, err := config.Load(config.Worker, without(without(workerEnvironment(), "EMAIL_SENDER"), "EMAIL_SMTP_ADDRESS"))

	require.EqualError(t, err, "configuration: EMAIL_SENDER is required")
}

func TestOnlyTheWorkerNeedsTheKeyOfResend(t *testing.T) {
	environment := with(validEnvironment(), "EMAIL_SENDER", "resend")

	_, serve := config.Load(config.ServeAPI, environment)
	_, migrate := config.Load(config.Migrate, environment)
	_, issue := config.Load(config.IssueSetupCode, environment)
	_, seed := config.Load(config.Seed, environment)

	assert.NoError(t, serve)
	assert.NoError(t, migrate)
	assert.NoError(t, issue)
	assert.NoError(t, seed)
}

// The secret of the webhook is optional: with none, the API takes no notice
// of a bounce.
func TestTheAPIReadsTheSecretOfTheWebhookOfResendWhenItIsSet(t *testing.T) {
	key := []byte("the signing key of the webhook!!")
	withSecret, err := config.Load(config.ServeAPI,
		with(validEnvironment(), "RESEND_WEBHOOK_SECRET", "whsec_"+base64.StdEncoding.EncodeToString(key)))
	require.NoError(t, err)
	withoutSecret, err := config.Load(config.ServeAPI, validEnvironment())
	require.NoError(t, err)
	empty, err := config.Load(config.ServeAPI, with(validEnvironment(), "RESEND_WEBHOOK_SECRET", ""))
	require.NoError(t, err)

	assert.True(t, withSecret.ResendWebhookSecret.IsSet())
	assert.Equal(t, key, withSecret.ResendWebhookSecret.Key())
	assert.False(t, withoutSecret.ResendWebhookSecret.IsSet())
	assert.False(t, empty.ResendWebhookSecret.IsSet())
}

func TestALoadedConfigurationNeverPrintsTheSecretsOfResend(t *testing.T) {
	environment := validEnvironment()
	environment["RESEND_API_KEY"] = "re_printed_key_of_resend"
	environment["RESEND_WEBHOOK_SECRET"] = "whsec_" + base64.StdEncoding.EncodeToString([]byte("printed-signing-key"))
	loaded, err := config.Load(config.ServeAPI, environment)
	require.NoError(t, err)

	for _, printed := range []string{
		fmt.Sprint(loaded), fmt.Sprintf("%+v", loaded), fmt.Sprintf("%#v", loaded),
		loaded.ResendAPIKey.String(), loaded.ResendWebhookSecret.String(),
	} {
		assert.NotContains(t, printed, "re_printed_key_of_resend")
		assert.NotContains(t, printed, "printed-signing-key")
		assert.NotContains(t, printed, base64.StdEncoding.EncodeToString([]byte("printed-signing-key")))
		assert.NotContains(t, printed, "112 114 105 110 116", "the key as bytes")
	}
}

func TestASecretOfAWebhookHasAKey(t *testing.T) {
	_, err := config.NewWebhookSecret("whsec_")

	require.Error(t, err)
}
