package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// The variables file of a server holds a value for every name of
// .env.example, and Compose reads it for the values the Compose file of the
// environment names (Secrets in Environments in CODING_STANDARDS.md). Each
// value is one of four kinds, decided for each name in stagingValues. A name
// of .env.example with no decision stops the publish before it logs in.

// value gives the staging value of one name of .env.example, for one
// publish.
type value func(publication) (string, error)

// fixed is the same value on every publish.
func fixed(text string) value {
	return func(publication) (string, error) { return text, nil }
}

// derived is derived from what the publish is given: the address of the
// server or the commit. The hash of the password of Mailpit, which can fail,
// is a value of its own, mailpitPasswordHash.
func derived(from func(publication) string) value {
	return func(given publication) (string, error) { return from(given), nil }
}

// random is drawn anew on each publish, and written only to the server, so
// that it lasts no longer than the data it protects.
func random(draw func() (string, error)) value {
	return func(publication) (string, error) { return draw() }
}

// deliberatelyEmpty is a name that nothing on the server reads, or one the
// Compose file of the environment writes itself.
func deliberatelyEmpty(publication) (string, error) {
	return "", nil
}

// seedMailbox is the mailbox the users of the seed are derived from in
// staging: the fictitious default of .env.example, since Mailpit receives
// every e-mail there (ADR-0021).
const seedMailbox = "seed@fabrica.example"

// stagingValues are the decisions of staging, one for each name of
// .env.example. A new name of .env.example gets its decision here.
var stagingValues = map[string]value{
	// PostgreSQL. Staging publishes no port but Caddy's.
	"POSTGRES_PORT":               deliberatelyEmpty,
	"POSTGRES_PASSWORD":           random(hexKey),
	"POSTGRES_MIGRATION_PASSWORD": random(hexKey),
	"POSTGRES_API_PASSWORD":       random(hexKey),

	// Caddy, which publishes 80 and 443 itself. Without an address for the
	// certificate authorities, Caddy asks Let's Encrypt alone.
	"CADDY_PORT": deliberatelyEmpty,
	"ACME_EMAIL": deliberatelyEmpty,

	// The images of the commit published.
	"COMMIT": derived(func(given publication) string { return given.commit }),

	// Garage, which the API reaches on the network of the project.
	"GARAGE_PORT":       deliberatelyEmpty,
	"GARAGE_RPC_SECRET": random(hexKey),

	// Mailpit, at its host name, behind the password of Mailpit, of which
	// Caddy holds only the hash.
	"MAILPIT_SMTP_PORT":     deliberatelyEmpty,
	"MAILPIT_PORT":          deliberatelyEmpty,
	"MAILPIT_PUBLIC_URL":    derived(func(given publication) string { return "https://" + hostNamesOf(given.address).mail }),
	"MAILPIT_PASSWORD_HASH": mailpitPasswordHash,

	// Dex, the River panel and Vite, which run in development alone.
	"DEX_PORT":         deliberatelyEmpty,
	"RIVER_PANEL_PORT": deliberatelyEmpty,
	"WEB_LISTEN_HOST":  deliberatelyEmpty,

	// The API binary. The Compose file writes where it listens, the
	// connections from the passwords of the roles, and where Garage is; no
	// telemetry runs in staging.
	"API_LISTEN_ADDRESS":                   deliberatelyEmpty,
	"SHUTDOWN_DRAIN_DEADLINE":              fixed("20s"),
	"LOG_LEVEL":                            fixed("info"),
	"OTEL_EXPORTER_OTLP_ENDPOINT":          deliberatelyEmpty,
	"DATABASE_URL":                         deliberatelyEmpty,
	"MIGRATION_DATABASE_URL":               deliberatelyEmpty,
	"DATABASE_POOL_MAX_SIZE":               fixed("10"),
	"DATABASE_STATEMENT_TIMEOUT":           fixed("30s"),
	"DATABASE_IDLE_IN_TRANSACTION_TIMEOUT": fixed("1m"),
	"HEALTH_CHECK_TIMEOUT":                 fixed("2s"),
	"APP_ORIGIN":                           derived(func(given publication) string { return "https://" + hostNamesOf(given.address).app }),
	"FILE_STORAGE_URL":                     deliberatelyEmpty,
	"FILE_STORAGE_PUBLIC_URL":              derived(func(given publication) string { return "https://" + hostNamesOf(given.address).files }),
	"FILE_STORAGE_REGION":                  fixed("garage"),
	"FILE_STORAGE_BUCKET":                  fixed("software-factory-files"),
	"FILE_STORAGE_ACCESS_KEY_ID":           random(garageKeyID),
	"FILE_STORAGE_SECRET_ACCESS_KEY":       random(hexKey),

	// The login: a login code read in Mailpit, and no local login provider.
	"LOGIN_CODE_KEY":                     random(hexKey),
	"LOCAL_LOGIN_PROVIDER_KEY":           deliberatelyEmpty,
	"LOCAL_LOGIN_PROVIDER_DISPLAY_NAME":  deliberatelyEmpty,
	"LOCAL_LOGIN_PROVIDER_ISSUER_URL":    deliberatelyEmpty,
	"LOCAL_LOGIN_PROVIDER_CLIENT_ID":     deliberatelyEmpty,
	"LOCAL_LOGIN_PROVIDER_CLIENT_SECRET": deliberatelyEmpty,

	// The seed.
	"SEED_MAILBOX": fixed(seedMailbox),

	// The worker, which sends to Mailpit, as the Compose file writes.
	"WORKER_LIGHT_QUEUE_MAX_JOBS": fixed("10"),
	"WORKER_HEAVY_QUEUE_MAX_JOBS": fixed("2"),
	"WORKER_SHUTDOWN_DEADLINE":    fixed("20s"),
	"EMAIL_SENDER":                deliberatelyEmpty,
	"EMAIL_SMTP_ADDRESS":          deliberatelyEmpty,
	"EMAIL_FROM_ADDRESS":          fixed("no-reply@fabrica.example"),
	"EMAIL_FROM_NAME":             fixed("My Software Factory Manager"),

	// Resend, which only production sends through.
	"RESEND_API_KEY":        deliberatelyEmpty,
	"RESEND_WEBHOOK_SECRET": deliberatelyEmpty,

	// The Go tests, which never run on a server.
	"TEST_DATABASE_ADMIN_URL": deliberatelyEmpty,
	"RESEND_TEST_API_KEY":     deliberatelyEmpty,
}

// variablesFile writes the variables file of the server: every name of the
// .env.example of the checkout, in its order, with its staging value. A name
// with no decision stops the publish here.
func (e environment) variablesFile(given publication) ([]byte, error) {
	example := filepath.Join(e.checkout.root, ".env.example")
	names, err := namesIn(example)
	if err != nil {
		return nil, fmt.Errorf("read the names of .env.example: %w", err)
	}
	undecided := slices.DeleteFunc(slices.Clone(names), func(name string) bool {
		_, decided := stagingValues[name]
		return decided
	})
	if len(undecided) > 0 {
		return nil, fmt.Errorf("no value is decided for %s of these names of .env.example: %s. "+
			"Decide each in apps/api/tools/environment/variables.go, as fixed, derived from the publish, drawn at random or deliberately empty",
			e.name, strings.Join(undecided, ", "))
	}
	var file strings.Builder
	fmt.Fprintf(&file, "# The variables of %s, written by the publish of commit %s, of pull request #%d.\n"+
		"# A name of .env.example left empty is one that nothing reads on %s, or one\n"+
		"# that its Compose file writes itself.\n", e.name, given.commit, given.pullRequest, e.name)
	var failed error
	for _, name := range names {
		text, err := stagingValues[name](given)
		failed = errors.Join(failed, failedTo("the value of "+name, err), quotable(name, text))
		file.WriteString(variableLine(name, text))
	}
	if failed != nil {
		return nil, fmt.Errorf("write the variables file: %w", failed)
	}
	return []byte(file.String()), nil
}

// variableLine writes one variable as Compose reads it: a value in single
// quotes is taken as it is, its dollar signs included.
func variableLine(name, text string) string {
	if text == "" {
		return name + "=\n"
	}
	return name + "='" + text + "'\n"
}

// quotable fails for a value that single quotes cannot hold.
func quotable(name, text string) error {
	if strings.ContainsAny(text, "'\r\n") {
		return fmt.Errorf("the value of %s holds a quote or a line break", name)
	}
	return nil
}

// hexKey draws 32 bytes, written in hexadecimal: a password, a secret of
// Garage, a key of the storage or of the login code.
func hexKey() (string, error) {
	return randomHex(32)
}

// garageKeyID draws the identifier of a key of Garage, which Garage writes
// as GK and 12 bytes in hexadecimal.
func garageKeyID() (string, error) {
	drawn, err := randomHex(12)
	return "GK" + drawn, err
}

func randomHex(size int) (string, error) {
	drawn := make([]byte, size)
	if _, err := rand.Read(drawn); err != nil {
		return "", err
	}
	return hex.EncodeToString(drawn), nil
}

// mailpitPasswordHash is derived from the password of Mailpit: its bcrypt
// hash, which Caddy checks the password the browser sends against. bcrypt
// refuses a password longer than 72 bytes.
func mailpitPasswordHash(given publication) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(given.mailpitPassword.Reveal()), bcrypt.DefaultCost)
	return string(hash), err
}

// seedAdministrator is the address the administrator of the seed logs in
// with: the mailbox of the seed, tagged +admin.
func seedAdministrator() string {
	local, domain, _ := strings.Cut(seedMailbox, "@")
	return local + "+admin@" + domain
}
