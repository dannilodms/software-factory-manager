package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
)

// A publish runs as the pipeline runs it, with the variables the job gives
// it, against a server, an app and a GitHub kept in memory.

const (
	// publishedCommit is the commit the tests publish.
	publishedCommit = "3f2c9a1b7d4e5f60718293a4b5c6d7e8f9012345"
	// mailpitPasswordOfTheTests is the password of Mailpit the tests give.
	mailpitPasswordOfTheTests = "MAILPITPASSWORDONLYTESTSKNOW"
	// compose is how the server runs Compose on the files of staging.
	compose = "docker compose --file staging/compose.staging.yaml --env-file staging/.env"
)

// readyToPublish opens pull request #12 with the staging label and gives the
// publish the variables the job gives it for that pull request, from the
// GitHub Environment of a staging that is on at 203.0.113.10, and nothing
// else: no token of the provider. The deploy key comes without its last
// line break, as gh keeps it.
func (w *world) readyToPublish() serverKeys {
	w.t.Helper()
	keys, err := newServerKeys("staging")
	require.NoError(w.t, err)
	w.github.AddPullRequest(aPullRequest(12, "Publish to staging"), "staging")
	w.variables = map[string]string{
		"COMMIT":           publishedCommit,
		"PULL_REQUEST":     "12",
		"SERVER_ADDRESS":   "203.0.113.10",
		"SERVER_HOST_KEY":  knownhosts.Line([]string{"203.0.113.10"}, keys.hostPublic),
		"DEPLOY_KEY":       strings.TrimRight(keys.deployPrivate.Reveal(), "\n"),
		"MAILPIT_PASSWORD": mailpitPasswordOfTheTests,
	}
	return keys
}

func TestAPublishRunsItsStepsOnTheServerInTheirOrder(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	world.succeeds("publish", "staging")

	assert.Equal(t, []string{
		// 2. The files the Compose file of staging reads, beside it.
		"write staging/compose.base.yaml 0644",
		"write staging/compose.staging.yaml 0644",
		"write staging/Caddyfile.staging 0644",
		"write staging/delivery.caddyfile 0644",
		// 3.
		"write staging/.env 0600",
		// 4.
		"run " + compose + " pull --quiet",
		// 5.
		"run " + compose + " stop api worker",
		// 6. A volume in use cannot be removed: its container goes first.
		// Mailpit keeps its e-mails inside its container, so it goes too,
		// and no login code of an earlier pull request is left.
		"run " + compose + " rm --stop --force postgres garage mailpit",
		"run docker volume rm --force software-factory-manager-staging_postgres-data software-factory-manager-staging_garage-data",
		// 7.
		"run " + compose + " up --detach --wait postgres garage garage-cors mailpit caddy",
		// 8.
		"run " + compose + " run --rm --no-deps -T migrate",
		"run " + compose + " run --rm --no-deps -T seed",
		// 9.
		"run " + compose + " up --detach --wait --no-deps api worker",
		// 10.
		"wait until https://app.203-0-113-10.sslip.io/up answers",
	}, world.journal.entries)
	assert.True(t, world.server.closed, "the publish logs out")
}

func TestAPublishLogsInAsTheDeployUserTrustingOnlyTheHostKeyItIsGiven(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	keys := world.readyToPublish()

	world.succeeds("publish", "staging")

	require.Len(t, world.server.targets, 1)
	target := world.server.targets[0]
	assert.Equal(t, "203.0.113.10:22", target.Address)
	assert.Equal(t, "deploy", target.User)
	assert.Equal(t, authorizedLine(keys.hostPublic), authorizedLine(target.HostKey))
	assert.Equal(t, keys.deployPublic, authorizedLine(target.Key.PublicKey()), "the deploy key reads without its last line break")
}

// variablesFileLine reads one line of a variables file: a name, and a value
// that is empty or in single quotes.
var variablesFileLine = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=(|'[^']*')$`)

// variablesOnServer reads the variables file the publish wrote.
func (w *world) variablesOnServer() map[string]string {
	w.t.Helper()
	file, found := w.server.files["staging/.env"]
	require.True(w.t, found, "the variables file is written")
	assert.Equal(w.t, fs.FileMode(0o600), file.mode, "only the deploy user reads it")
	values := map[string]string{}
	for line := range strings.Lines(string(file.content)) {
		w.readVariable(values, strings.TrimSuffix(line, "\n"))
	}
	return values
}

// readVariable reads the variable a line of the file sets, unless the line
// is a comment.
func (w *world) readVariable(values map[string]string, line string) {
	w.t.Helper()
	if strings.HasPrefix(line, "#") {
		return
	}
	parts := variablesFileLine.FindStringSubmatch(line)
	require.NotNil(w.t, parts, "a line Compose reads as it is: %q", line)
	values[parts[1]] = strings.Trim(parts[2], "'")
}

// randomlyDrawn are the names of .env.example whose staging value is drawn
// anew on each publish, and the form of each.
var randomlyDrawn = map[string]string{
	"POSTGRES_PASSWORD":              `^[0-9a-f]{64}$`,
	"POSTGRES_MIGRATION_PASSWORD":    `^[0-9a-f]{64}$`,
	"POSTGRES_API_PASSWORD":          `^[0-9a-f]{64}$`,
	"GARAGE_RPC_SECRET":              `^[0-9a-f]{64}$`,
	"FILE_STORAGE_ACCESS_KEY_ID":     `^GK[0-9a-f]{24}$`,
	"FILE_STORAGE_SECRET_ACCESS_KEY": `^[0-9a-f]{64}$`,
	"LOGIN_CODE_KEY":                 `^[0-9a-f]{64}$`,
}

func TestTheVariablesFileHasAStagingValueForEveryNameOfEnvExample(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	world.succeeds("publish", "staging")

	values := world.variablesOnServer()
	example, err := namesIn(filepath.Join(repository, ".env.example"))
	require.NoError(t, err)
	assert.ElementsMatch(t, example, slices.Collect(maps.Keys(values)), "every name of .env.example, and no other")
	for name, form := range randomlyDrawn {
		assert.Regexp(t, form, values[name], "%s is drawn at random", name)
		delete(values, name)
	}
	hash := values["MAILPIT_PASSWORD_HASH"]
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(mailpitPasswordOfTheTests)), "Caddy checks the password of Mailpit against its hash")
	delete(values, "MAILPIT_PASSWORD_HASH")
	assert.Equal(t, map[string]string{
		// Derived from the publish.
		"APP_ORIGIN":              "https://app.203-0-113-10.sslip.io",
		"FILE_STORAGE_PUBLIC_URL": "https://files.203-0-113-10.sslip.io",
		"MAILPIT_PUBLIC_URL":      "https://mail.203-0-113-10.sslip.io",
		"COMMIT":                  publishedCommit,
		// Fixed.
		"SHUTDOWN_DRAIN_DEADLINE":              "20s",
		"LOG_LEVEL":                            "info",
		"DATABASE_POOL_MAX_SIZE":               "10",
		"DATABASE_STATEMENT_TIMEOUT":           "30s",
		"DATABASE_IDLE_IN_TRANSACTION_TIMEOUT": "1m",
		"HEALTH_CHECK_TIMEOUT":                 "2s",
		"FILE_STORAGE_REGION":                  "garage",
		"FILE_STORAGE_BUCKET":                  "software-factory-files",
		"SEED_MAILBOX":                         "seed@fabrica.example",
		"WORKER_LIGHT_QUEUE_MAX_JOBS":          "10",
		"WORKER_HEAVY_QUEUE_MAX_JOBS":          "2",
		"WORKER_SHUTDOWN_DEADLINE":             "20s",
		"EMAIL_FROM_ADDRESS":                   "no-reply@fabrica.example",
		"EMAIL_FROM_NAME":                      "My Software Factory Manager",
		// Deliberately empty: development only, written by the Compose file
		// of staging, no telemetry, no certificate address yet, no local
		// login provider, no Resend, the tests.
		"POSTGRES_PORT": "", "CADDY_PORT": "", "GARAGE_PORT": "", "MAILPIT_SMTP_PORT": "", "MAILPIT_PORT": "",
		"DEX_PORT": "", "RIVER_PANEL_PORT": "", "WEB_LISTEN_HOST": "",
		"API_LISTEN_ADDRESS": "", "DATABASE_URL": "", "MIGRATION_DATABASE_URL": "", "FILE_STORAGE_URL": "",
		"EMAIL_SENDER": "", "EMAIL_SMTP_ADDRESS": "",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "", "ACME_EMAIL": "",
		"LOCAL_LOGIN_PROVIDER_KEY": "", "LOCAL_LOGIN_PROVIDER_DISPLAY_NAME": "", "LOCAL_LOGIN_PROVIDER_ISSUER_URL": "",
		"LOCAL_LOGIN_PROVIDER_CLIENT_ID": "", "LOCAL_LOGIN_PROVIDER_CLIENT_SECRET": "",
		"RESEND_API_KEY": "", "RESEND_WEBHOOK_SECRET": "",
		"TEST_DATABASE_ADMIN_URL": "", "RESEND_TEST_API_KEY": "",
	}, values)
}

func TestEachPublishDrawsItsSecretsAnew(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.succeeds("publish", "staging")
	first := world.variablesOnServer()

	world.succeeds("publish", "staging")

	second := world.variablesOnServer()
	for name := range randomlyDrawn {
		assert.NotEqual(t, first[name], second[name], "%s is drawn anew", name)
	}
	assert.NotEqual(t, first["MAILPIT_PASSWORD_HASH"], second["MAILPIT_PASSWORD_HASH"], "the hash has a salt of its own each time")
	assert.Equal(t, first["APP_ORIGIN"], second["APP_ORIGIN"])
}

func TestANameOfEnvExampleWithNoDecisionStopsThePublishBeforeItLogsIn(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	example := filepath.Join(world.directory, ".env.example")
	content, err := os.ReadFile(example)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(example, append(content, []byte("# A setting nobody decided for staging.\nNEW_SETTING=on\n")...), 0o600))

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: no value is decided for staging of these names of .env.example: NEW_SETTING. "+
		"Decide each in apps/api/tools/environment/variables.go, as fixed, derived from the publish, drawn at random or deliberately empty\n", ran.stderr)
	assert.Empty(t, world.server.targets, "the server was not logged in to")
	assert.Empty(t, world.journal.entries, "nothing was done on the server")
	assert.Empty(t, world.github.Comments(12))
}

func TestThePublishedPullRequestGetsACommentWithTheAddressesOfTheAppAndOfMailpit(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	world.succeeds("publish", "staging")

	assert.Equal(t, []string{
		"Staging shows this pull request, at commit " + publishedCommit + ".\n\n" +
			"- The app: https://app.203-0-113-10.sslip.io\n" +
			"- Mailpit: https://mail.203-0-113-10.sslip.io, as the user `maintainer`, with the password `STAGING_MAILPIT_PASSWORD` of the `.env` of the main checkout\n\n" +
			"Its data is the seed's: log in as `seed+admin@fabrica.example` with a login code, which Mailpit receives.\n",
	}, world.github.Comments(12))
}

// aPullRequest is an open pull request of the repository.
func aPullRequest(number int, title string) github.PullRequest {
	return github.PullRequest{Number: number, Title: title, URL: fmt.Sprintf("https://github.com/dannilodms/software-factory-manager/pull/%d", number)}
}

// labelledNumbers are the numbers of the open pull requests that have the
// staging label.
func (w *world) labelledNumbers() []int {
	w.t.Helper()
	labelled, err := w.github.LabelledPullRequests(context.Background(), "staging")
	require.NoError(w.t, err)
	numbers := make([]int, 0, len(labelled))
	for _, pullRequest := range labelled {
		numbers = append(numbers, pullRequest.Number)
	}
	return numbers
}

// shownElsewhere is the comment on a pull request that lost the label to
// pull request #12.
const shownElsewhere = "Staging shows pull request #12 now, so the `staging` label came off this pull request, " +
	"and a push to it publishes nothing. Add the label again to publish it.\n"

func TestAPublishTakesTheLabelOffEveryOtherOpenPullRequestWithACommentThere(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.AddPullRequest(aPullRequest(7, "Shown before"), "staging", "documentation")
	world.github.AddPullRequest(aPullRequest(9, "Not on staging"), "documentation")
	world.readyToPublish()
	world.github.AddPullRequest(aPullRequest(15, "Also labelled"), "staging")

	world.succeeds("publish", "staging")

	assert.Equal(t, []int{12}, world.labelledNumbers(), "staging shows one pull request")
	assert.Equal(t, []string{"documentation"}, world.github.LabelsOf(7), "the other labels stay")
	assert.Equal(t, []string{shownElsewhere}, world.github.Comments(7))
	assert.Equal(t, []string{shownElsewhere}, world.github.Comments(15))
	assert.Equal(t, []string{"documentation"}, world.github.LabelsOf(9))
	assert.Empty(t, world.github.Comments(9), "a pull request without the label is left alone")
	assert.Len(t, world.github.Comments(12), 1, "the published one gets the addresses")
}

// The run of a push made before the label came off, by another publish or
// by hand, may reach its publish after: the label is read again then.
func TestAPullRequestThatLostTheLabelBeforeItsPublishStartedIsNotPublished(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.AddPullRequest(aPullRequest(14, "Shown now"), "staging")
	world.readyToPublish()
	require.NoError(t, world.github.RemoveLabel(context.Background(), 12, "staging"))

	printed := world.succeeds("publish", "staging")

	assert.Equal(t, "Pull request #12 does not have the staging label any more, so it is not published.\n", printed)
	assert.Empty(t, world.server.targets, "the server was not logged in to")
	assert.Empty(t, world.journal.entries)
	assert.Equal(t, []int{14}, world.labelledNumbers(), "the pull request staging shows keeps its label")
	assert.Empty(t, world.github.Comments(12))
	assert.Empty(t, world.github.Comments(14))
}

func TestAPullRequestThatWasClosedBeforeItsPublishStartedIsNotPublished(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.github.Close(12)

	printed := world.succeeds("publish", "staging")

	assert.Equal(t, "Pull request #12 does not have the staging label any more, so it is not published.\n", printed)
	assert.Empty(t, world.server.targets)
}

func TestAPublishThatCannotReadTheLabelsStopsBeforeItLogsIn(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.github.Unreachable(errors.New("gh failed, with exit status 1: HTTP 403"))

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: find whether pull request #12 still has the staging label: gh failed, with exit status 1: HTTP 403\n", ran.stderr)
	assert.Empty(t, world.server.targets)
}

func TestTheFilesOfTheServerAreTheCommittedOnes(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	world.succeeds("publish", "staging")

	for _, name := range []string{"compose.base.yaml", "compose.staging.yaml", "Caddyfile.staging", "delivery.caddyfile"} {
		committed, err := os.ReadFile(filepath.Join(repository, "deploy", name))
		require.NoError(t, err)
		assert.Equal(t, string(committed), string(world.server.files["staging/"+name].content), "%s is copied as committed", name)
	}
}

func TestTheAppHasFiveMinutesToAnswer(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	world.succeeds("publish", "staging")

	assert.Equal(t, 5*time.Minute, world.app.deadline, "the first publish to a server waits for its certificates too")
}

func TestAPublishNeedsNoTokenOfTheProvider(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	world.succeeds("publish", "staging")

	assert.Empty(t, world.tokens, "the provider was not reached")
}

func TestAPublishWithAnInputMissingOrUnreadableStopsBeforeItLogsIn(t *testing.T) {
	t.Parallel()
	otherKeys, err := newServerKeys("staging")
	require.NoError(t, err)
	for name, test := range map[string]struct {
		variable, value, failure string
	}{
		"no deploy key": {
			variable: "DEPLOY_KEY",
			failure:  "the publish needs DEPLOY_KEY, which the pipeline gives it from the pull request and from the GitHub Environment staging",
		},
		"no address, which a staging that is off has": {
			variable: "SERVER_ADDRESS",
			failure:  "the publish needs SERVER_ADDRESS, which the pipeline gives it from the pull request and from the GitHub Environment staging",
		},
		"a commit that is not a full hash": {
			variable: "COMMIT", value: "3f2c9a1",
			failure: "COMMIT is not the full hash of a commit",
		},
		"a pull request that is not a number": {
			variable: "PULL_REQUEST", value: "refs/pull/12/merge",
			failure: "PULL_REQUEST is not the number of a pull request",
		},
		"an address that is not IPv4": {
			variable: "SERVER_ADDRESS", value: "2001:db8::10",
			failure: "SERVER_ADDRESS is not an IPv4 address",
		},
		"a host key of another address": {
			variable: "SERVER_HOST_KEY", value: knownhosts.Line([]string{"198.51.100.7"}, otherKeys.hostPublic),
			failure: "SERVER_HOST_KEY is not a line of known hosts for SERVER_ADDRESS",
		},
		"a deploy key that is not one": {
			variable: "DEPLOY_KEY", value: "-----BEGIN OPENSSH PRIVATE KEY-----\nnot a key\n-----END OPENSSH PRIVATE KEY-----",
			failure: "DEPLOY_KEY is not a private key in OpenSSH's format",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			world := aWorld(t)
			world.readyToPublish()
			world.variables[test.variable] = test.value

			ran := world.run("publish", "staging")

			assert.Equal(t, 1, ran.status)
			assert.Equal(t, "environment: "+test.failure+"\n", ran.stderr)
			assert.Empty(t, world.server.targets, "the server was not logged in to")
		})
	}
}

func TestAStepThatFailsStopsThePublishAndSaysWhichStep(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.server.failWhere(" pull ", errors.New(`"docker compose pull --quiet" exited with status 18`))

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: step 4, pull the images of the commit: \"docker compose pull --quiet\" exited with status 18. "+
		"A new publish starts staging again from the beginning\n", ran.stderr)
	assert.Equal(t, "run "+compose+" pull --quiet", world.journal.entries[len(world.journal.entries)-1], "nothing runs after the step that failed")
	assert.True(t, world.server.closed)
	assert.Empty(t, world.github.Comments(12))
}

func TestAnAppThatNeverAnswersFailsThePublishAndGetsNoComment(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.app.err = errors.New("https://app.203-0-113-10.sslip.io/up did not answer 200 in time: it answered 502 Bad Gateway")

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: step 10, wait until https://app.203-0-113-10.sslip.io/up answers, for up to 5m0s: "+
		"https://app.203-0-113-10.sslip.io/up did not answer 200 in time: it answered 502 Bad Gateway. "+
		"A new publish starts staging again from the beginning\n", ran.stderr)
	assert.Empty(t, world.github.Comments(12))
}

func TestAServerThatRefusesTheLoginIsNotWorkedOn(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.server.refusal = sshclient.ErrOtherHostKey

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: step 1, log in to the server at 203.0.113.10: the server answered with another host key\n", ran.stderr)
	assert.Empty(t, world.journal.entries)
}

func TestACommentThatFailsFailsThePublishOnceStagingShowsThePullRequest(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()
	world.github.FailCommentingOn(12, errors.New("comment on the pull request #12: gh failed, with exit status 1: HTTP 403"))

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: staging shows pull request #12, but it got no comment with the addresses: "+
		"comment on the pull request #12: gh failed, with exit status 1: HTTP 403\n", ran.stderr)
	assert.Contains(t, world.journal.entries, "wait until https://app.203-0-113-10.sslip.io/up answers")
}

func TestALabelThatDoesNotComeOffFailsThePublishOnceEverythingElseWasTried(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.AddPullRequest(aPullRequest(7, "Shown before"), "staging")
	world.github.AddPullRequest(aPullRequest(8, "Labelled too"), "staging")
	world.readyToPublish()
	world.github.FailRemovingALabelFrom(7, errors.New("take the label staging off the pull request #7: gh failed, with exit status 1: HTTP 403"))
	world.github.FailCommentingOn(8, errors.New("comment on the pull request #8: gh failed, with exit status 1: HTTP 403"))

	ran := world.run("publish", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "environment: staging shows pull request #12, but "+
		"the staging label is still on pull request #7: take the label staging off the pull request #7: gh failed, with exit status 1: HTTP 403; "+
		"pull request #8 lost the staging label, with no comment saying why: comment on the pull request #8: gh failed, with exit status 1: HTTP 403\n", ran.stderr)
	assert.Equal(t, []int{7, 12}, world.labelledNumbers())
	assert.Empty(t, world.github.Comments(7))
	assert.Len(t, world.github.Comments(12), 1, "the published one still gets the addresses")
}

func TestAPublishSaysOffWhichPullRequestsItTookTheLabel(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.AddPullRequest(aPullRequest(7, "Shown before"), "staging")
	world.readyToPublish()

	printed := world.succeeds("publish", "staging")

	assert.True(t, strings.HasSuffix(printed, "Staging shows pull request #12.\n"+
		"  App:    https://app.203-0-113-10.sslip.io\n"+
		"  Files:  https://files.203-0-113-10.sslip.io\n"+
		"  Mail:   https://mail.203-0-113-10.sslip.io, as the user maintainer\n"+
		"Took the staging label off pull request #7, with a comment there.\n"+
		"Commented the addresses on pull request #12.\n"), printed)
}

func TestAPublishSaysEachOfItsSteps(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.readyToPublish()

	printed := world.succeeds("publish", "staging")

	assert.Equal(t, `Publishing commit `+publishedCommit+` of pull request #12 to staging, at 203.0.113.10.
Every name of .env.example has a value for staging.
1. Log in to the server as deploy, trusting only the host key staging was given.
2. Copy the Compose files and the configuration of Caddy.
3. Write the variables file, readable by the deploy user alone.
4. Pull the images of the commit.
5. Stop the API and the worker.
6. Remove PostgreSQL, Garage and Mailpit, with their data.
7. Start PostgreSQL, Garage and its rule, Mailpit and Caddy.
8. Run the migration, then the seed.
9. Start the API and the worker.
10. Wait until https://app.203-0-113-10.sslip.io/up answers, for up to 5m0s.
Staging shows pull request #12.
  App:    https://app.203-0-113-10.sslip.io
  Files:  https://files.203-0-113-10.sslip.io
  Mail:   https://mail.203-0-113-10.sslip.io, as the user maintainer
Commented the addresses on pull request #12.
`, printed)
}

func TestAPublishPrintsNoValueAndRunsNoCommandThatCarriesOne(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	keys := world.readyToPublish()
	var printed strings.Builder
	record := func(ran result) {
		printed.WriteString(ran.stdout)
		printed.WriteString(ran.stderr)
	}
	record(world.run("publish", "staging"))
	values := world.variablesOnServer()
	world.server.failWhere(" seed", errors.New("the seed failed"))
	record(world.run("publish", "staging"))

	secrets := map[string]string{
		"the password of Mailpit":        mailpitPasswordOfTheTests,
		"the deploy key":                 strings.Split(keys.deployPrivate.Reveal(), "\n")[1],
		"the hash of the password":       values["MAILPIT_PASSWORD_HASH"],
		"the variables file, as written": string(world.server.files["staging/.env"].content),
	}
	for name := range randomlyDrawn {
		secrets[name] = values[name]
	}
	said := printed.String() + strings.Join(world.journal.entries, "\n") + strings.Join(world.github.Comments(12), "\n")
	for name, secret := range secrets {
		require.NotEmpty(t, secret, name)
		assert.NotContains(t, said, secret, "%s is printed, in a command or in the comment", name)
	}
}
