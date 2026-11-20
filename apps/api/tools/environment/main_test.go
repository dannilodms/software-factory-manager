package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github/githubtest"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider/providertest"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/sshclient"
)

// The commands run as the process would, on a checkout written into a
// temporary directory with the committed files of this repository that they
// read, against a provider, a readiness, a GitHub, a server and an app kept
// in memory.

// repository is the root of this repository, from the directory of the
// package.
const repository = "../../../.."

// token is the token of the provider the tests give the commands.
const token = "hetzner-token-only-the-tests-know"

// committed are the files of the repository the commands read.
var committed = []string{
	"deploy/server.staging.yaml", "deploy/cloud-init.yaml", ".env.example",
	"deploy/compose.base.yaml", "deploy/compose.staging.yaml", "deploy/Caddyfile.staging", "deploy/delivery.caddyfile",
}

// clock stands still until the test moves it.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// fakeReadiness answers what the test tells it and keeps the servers it
// was asked to wait for.
type fakeReadiness struct {
	err     error
	targets []sshclient.Target
}

func (r *fakeReadiness) Wait(_ context.Context, target sshclient.Target) error {
	r.targets = append(r.targets, target)
	return r.err
}

// journal is what was done on the server of an environment, and the wait
// for its app, in order.
type journal struct{ entries []string }

func (j *journal) add(entry string) { j.entries = append(j.entries, entry) }

// written is a file written on the server.
type written struct {
	content []byte
	mode    fs.FileMode
}

// fakeServer stands for the server of an environment. It keeps the files
// written, records each file and each command in the journal, and fails
// the commands that contain what the test says.
type fakeServer struct {
	journal *journal
	files   map[string]written
	failing []failure
	// refusal is the answer to a login, when the server refuses it.
	refusal error
	// targets are how the server was logged in to.
	targets []sshclient.Target
	closed  bool
}

func (s *fakeServer) WriteFile(_ context.Context, name string, content []byte, mode fs.FileMode) error {
	s.journal.add(fmt.Sprintf("write %s %04o", name, mode))
	s.files[name] = written{content: slices.Clone(content), mode: mode}
	return nil
}

func (s *fakeServer) Run(_ context.Context, command string) error {
	s.journal.add("run " + command)
	found := slices.IndexFunc(s.failing, func(f failure) bool { return strings.Contains(command, f.part) })
	if found < 0 {
		return nil
	}
	return s.failing[found].err
}

func (s *fakeServer) Close() error {
	s.closed = true
	return nil
}

// failure makes the commands that contain the part fail with err.
type failure struct {
	part string
	err  error
}

// failWhere makes every command that contains the part fail with err.
func (s *fakeServer) failWhere(part string, err error) {
	s.failing = append(s.failing, failure{part: part, err: err})
}

// fakeApp stands for the app of a server: it records the wait in the
// journal and answers what the test says.
type fakeApp struct {
	journal  *journal
	err      error
	deadline time.Duration
}

func (a *fakeApp) WaitUntilUp(ctx context.Context, url string) error {
	a.journal.add("wait until " + url + " answers")
	deadline, _ := ctx.Deadline()
	a.deadline = time.Until(deadline).Round(time.Minute)
	return a.err
}

// world is what the commands of one test reach.
type world struct {
	t         *testing.T
	directory string
	provider  *providertest.Provider
	readiness *fakeReadiness
	github    *githubtest.GitHub
	server    *fakeServer
	app       *fakeApp
	journal   *journal
	clock     *clock
	variables map[string]string
	// tokens are the tokens the provider was reached with.
	tokens []config.Secret
	labels []map[string]string
}

// aWorld writes a main checkout of the repository and runs the commands in
// it, with the token in the variables of the process.
func aWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	copyCommitted(t, root)
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o700))
	now := &clock{now: time.Date(2026, 11, 19, 14, 0, 0, 0, time.UTC)}
	done := &journal{}
	return &world{
		t:         t,
		directory: root,
		provider:  providertest.NewProvider(now.Now),
		readiness: &fakeReadiness{},
		github:    githubtest.NewGitHub(),
		server:    &fakeServer{journal: done, files: map[string]written{}},
		app:       &fakeApp{journal: done},
		journal:   done,
		clock:     now,
		variables: map[string]string{"HETZNER_TOKEN": token},
	}
}

func copyCommitted(t *testing.T, root string) {
	t.Helper()
	for _, file := range committed {
		content, err := os.ReadFile(filepath.Join(repository, file))
		require.NoError(t, err)
		target := filepath.Join(root, filepath.FromSlash(file))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		require.NoError(t, os.WriteFile(target, content, 0o600))
	}
}

func (w *world) outside() outside {
	return outside{
		directory: w.directory,
		connect: func(_ string, token config.Secret, labels map[string]string) provider.Provider {
			w.tokens = append(w.tokens, token)
			w.labels = append(w.labels, labels)
			return w.provider
		},
		readiness: w.readiness,
		github:    w.github,
		reachServer: func(_ context.Context, target sshclient.Target, _ io.Writer) (remoteServer, error) {
			w.server.targets = append(w.server.targets, target)
			if w.server.refusal != nil {
				return nil, w.server.refusal
			}
			return w.server, nil
		},
		app: w.app,
		now: w.clock.Now,
	}
}

// result is what one command printed, and its exit status.
type result struct {
	status         int
	stdout, stderr string
}

func (w *world) run(arguments ...string) result {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	status := run(context.Background(), arguments, w.variables, w.outside(), &stdout, &stderr)
	return result{status: status, stdout: stdout.String(), stderr: stderr.String()}
}

// succeeds runs a command that must succeed and print no error.
func (w *world) succeeds(arguments ...string) string {
	w.t.Helper()
	ran := w.run(arguments...)
	require.Equal(w.t, 0, ran.status, ran.stderr)
	require.Empty(w.t, ran.stderr)
	return ran.stdout
}

// localCopy is the folder where the main checkout keeps the keys of
// staging.
func (w *world) localCopy() string {
	return filepath.Join(w.directory, ".servers", "staging")
}

// specification is what the server of staging was created from.
func (w *world) specification() provider.Specification {
	w.t.Helper()
	specification, found := w.provider.SpecificationOf("staging")
	require.True(w.t, found, "the server of staging exists")
	return specification
}
