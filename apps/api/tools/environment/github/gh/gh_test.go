package gh_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github/gh"
)

// The adapter runs a stand-in of gh: this test binary, run again with
// standInVariable set. The stand-in records each call, its arguments and
// what it read on its standard input, in the directory it runs in, which is
// the directory of one test, and answers as that directory says.

// standInVariable makes the test binary the stand-in of gh.
const standInVariable = "ENVIRONMENT_TOOL_GH_STAND_IN"

const (
	callsFile   = "calls.jsonl"
	answersFile = "answers.json"
)

// call is one run of the stand-in.
type call struct {
	Arguments []string `json:"arguments"`
	Input     string   `json:"input"`
}

// answer is what the stand-in answers a call whose arguments start with
// those.
type answer struct {
	Arguments []string `json:"arguments"`
	Status    int      `json:"status"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
}

// TestMain reads the environment as a command of the binary does, through
// config.Environment, the one reader of the environment.
func TestMain(m *testing.M) {
	if config.Environment()[standInVariable] != "" {
		os.Exit(standIn())
	}
	if err := os.Setenv(standInVariable, "1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// standIn records the call and answers it. A call with no answer succeeds
// and prints nothing.
func standIn() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 99
	}
	if err := record(call{Arguments: os.Args[1:], Input: string(input)}); err != nil {
		return 99
	}
	content, err := os.ReadFile(answersFile)
	if err != nil && !os.IsNotExist(err) {
		return 99
	}
	var answers []answer
	if len(content) > 0 && json.Unmarshal(content, &answers) != nil {
		return 99
	}
	found := slices.IndexFunc(answers, func(answer answer) bool { return startsWith(os.Args[1:], answer.Arguments) })
	if found < 0 {
		return 0
	}
	fmt.Fprint(os.Stdout, answers[found].Stdout)
	fmt.Fprint(os.Stderr, answers[found].Stderr)
	return answers[found].Status
}

func record(made call) error {
	line, err := json.Marshal(made)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(callsFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return errors.Join(err, file.Close())
}

func startsWith(arguments, prefix []string) bool {
	return len(arguments) >= len(prefix) && slices.Equal(arguments[:len(prefix)], prefix)
}

// fakeGh is the directory one test runs the stand-in in.
type fakeGh struct {
	t         *testing.T
	directory string
	given     []answer
}

func newFakeGh(t *testing.T) *fakeGh {
	t.Helper()
	return &fakeGh{t: t, directory: t.TempDir()}
}

// answers makes the calls whose arguments start with those answer so.
func (f *fakeGh) answers(status int, stdout, stderr string, arguments ...string) {
	f.t.Helper()
	f.given = append(f.given, answer{Arguments: arguments, Status: status, Stdout: stdout, Stderr: stderr})
	content, err := json.Marshal(f.given)
	require.NoError(f.t, err)
	require.NoError(f.t, os.WriteFile(filepath.Join(f.directory, answersFile), content, 0o600))
}

// calls gives what the stand-in was run with, in order.
func (f *fakeGh) calls() []call {
	f.t.Helper()
	file, err := os.Open(filepath.Join(f.directory, callsFile))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(f.t, err)
	defer file.Close()
	var calls []call
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		var made call
		require.NoError(f.t, json.Unmarshal(lines.Bytes(), &made))
		calls = append(calls, made)
	}
	require.NoError(f.t, lines.Err())
	return calls
}

// github is the adapter, running the stand-in in the directory of the
// test.
func (f *fakeGh) github() *gh.GitHub {
	f.t.Helper()
	executable, err := os.Executable()
	require.NoError(f.t, err)
	return gh.New(gh.Settings{Executable: executable, Directory: f.directory})
}

const deployKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz
c2gtZWQyNTUxOQAAACAadFT/xz7hB0ljAc3Jcd2LwNAt4C6ptp9QOa2E+G30RQAA
-----END OPENSSH PRIVATE KEY-----
`

func TestASecretGoesToGhThroughItsStandardInputAndNeverAsAnArgument(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)

	err := fake.github().SetSecret(context.Background(), "staging", "DEPLOY_KEY", config.NewSecret(deployKey))

	require.NoError(t, err)
	calls := fake.calls()
	assert.Equal(t, []call{{Arguments: []string{"secret", "set", "DEPLOY_KEY", "--env", "staging"}, Input: deployKey}}, calls)
	arguments := strings.Join(calls[0].Arguments, " ")
	for line := range strings.Lines(deployKey) {
		assert.NotContains(t, arguments, strings.TrimSpace(line), "a line of the secret is an argument")
	}
}

func TestAVariableGoesToGhThroughItsStandardInputToo(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	hostKey := "203.0.113.10 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFrWUXqvhfXGLR1iIzuT0/68nINm7RiMdocjXZbLw+sk"

	err := fake.github().SetVariable(context.Background(), "staging", "SERVER_HOST_KEY", hostKey)

	require.NoError(t, err)
	assert.Equal(t, []call{{Arguments: []string{"variable", "set", "SERVER_HOST_KEY", "--env", "staging"}, Input: hostKey}}, fake.calls())
}

const environmentsList = "repos/{owner}/{repo}/environments"

func TestMakingSureOfAnEnvironmentThatExistsWritesNothing(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(0, "production\nStaging\n", "", "api", environmentsList)

	err := fake.github().EnsureEnvironment(context.Background(), "staging")

	require.NoError(t, err)
	assert.Equal(t, []call{
		{Arguments: []string{"api", environmentsList, "--paginate", "--jq", ".environments[]?.name"}},
	}, fake.calls(), "GitHub compares the names of environments regardless of case")
}

func TestMakingSureOfAnEnvironmentThatDoesNotExistCreatesIt(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(0, "production\n", "", "api", environmentsList)

	err := fake.github().EnsureEnvironment(context.Background(), "staging")

	require.NoError(t, err)
	assert.Equal(t, []call{
		{Arguments: []string{"api", environmentsList, "--paginate", "--jq", ".environments[]?.name"}},
		{Arguments: []string{"api", "--method", "PUT", "repos/{owner}/{repo}/environments/staging", "--silent"}},
	}, fake.calls())
}

func TestMakingSureOfTheLabelCreatesItOrUpdatesIt(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)

	err := fake.github().EnsureLabel(context.Background(), github.Label{Name: "staging", Description: "Publish this pull request to staging", Color: "FBCA04"})

	require.NoError(t, err)
	assert.Equal(t, []call{{Arguments: []string{
		"label", "create", "staging", "--description", "Publish this pull request to staging", "--color", "FBCA04", "--force",
	}}}, fake.calls())
}

func TestDeletingWhatTheEnvironmentHasDeletesIt(t *testing.T) {
	t.Parallel()
	for kind, remove := range map[string]func(*gh.GitHub, string) error{
		"secret":   func(g *gh.GitHub, name string) error { return g.DeleteSecret(context.Background(), "staging", name) },
		"variable": func(g *gh.GitHub, name string) error { return g.DeleteVariable(context.Background(), "staging", name) },
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGh(t)
			fake.answers(0, "staging\n", "", "api", environmentsList)
			fake.answers(0, "DEPLOY_KEY\nSERVER_ADDRESS\n", "", kind, "list")

			err := remove(fake.github(), "SERVER_ADDRESS")

			require.NoError(t, err)
			assert.Equal(t, []call{
				{Arguments: []string{"api", environmentsList, "--paginate", "--jq", ".environments[]?.name"}},
				{Arguments: []string{kind, "list", "--env", "staging", "--json", "name", "--jq", ".[].name"}},
				{Arguments: []string{kind, "delete", "SERVER_ADDRESS", "--env", "staging"}},
			}, fake.calls())
		})
	}
}

func TestDeletingWhatDoesNotExistSucceedsAndDeletesNothing(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		environments, names string
		calls               int
	}{
		"no environment":                       {environments: "production\n", calls: 1},
		"an environment that does not have it": {environments: "staging\n", names: "DEPLOY_KEY\n", calls: 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGh(t)
			fake.answers(0, test.environments, "", "api", environmentsList)
			fake.answers(0, test.names, "", "variable", "list")
			fake.answers(1, "", "HTTP 404: Not Found", "variable", "delete")

			err := fake.github().DeleteVariable(context.Background(), "staging", "SERVER_ADDRESS")

			require.NoError(t, err)
			assert.Len(t, fake.calls(), test.calls)
		})
	}
}

// openPullRequests is GitHub's own list of the open pull requests, which
// is current at once. `gh pr list --label` asks GitHub's search instead,
// which can still list a pull request whose label came off a moment ago.
const openPullRequests = "repos/{owner}/{repo}/pulls?state=open&per_page=100"

func TestTheLabelledPullRequestsAreTheOpenOnesWithTheLabel(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(0, `{"number":11,"title":"Another change","url":"https://github.com/dannilodms/software-factory-manager/pull/11","labels":["documentation"]}
{"number":12,"title":"Publish to staging","url":"https://github.com/dannilodms/software-factory-manager/pull/12","labels":["staging"]}
{"number":13,"title":"Also labelled","url":"https://github.com/dannilodms/software-factory-manager/pull/13","labels":["documentation","Staging"]}
`, "", "api", openPullRequests)

	found, err := fake.github().LabelledPullRequests(context.Background(), "staging")

	require.NoError(t, err)
	assert.Equal(t, []github.PullRequest{
		{Number: 12, Title: "Publish to staging", URL: "https://github.com/dannilodms/software-factory-manager/pull/12"},
		{Number: 13, Title: "Also labelled", URL: "https://github.com/dannilodms/software-factory-manager/pull/13"},
	}, found, "GitHub compares the names of labels regardless of case")
	assert.Equal(t, []call{{Arguments: []string{
		"api", openPullRequests, "--paginate", "--jq", ".[] | {number, title, url: .html_url, labels: [.labels[].name]}",
	}}}, fake.calls())
}

func TestNoPullRequestWithTheLabel(t *testing.T) {
	t.Parallel()
	for name, listed := range map[string]string{
		"no open pull request":                    "",
		"no open pull request that has the label": `{"number":11,"title":"Another change","url":"https://github.com/dannilodms/software-factory-manager/pull/11","labels":[]}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGh(t)
			fake.answers(0, listed, "", "api", openPullRequests)

			found, err := fake.github().LabelledPullRequests(context.Background(), "staging")

			require.NoError(t, err)
			assert.Empty(t, found)
		})
	}
}

func TestAListThatIsNotReadableIsAFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(0, "<html>Unicorn!</html>\n", "", "api", openPullRequests)

	_, err := fake.github().LabelledPullRequests(context.Background(), "staging")

	require.ErrorContains(t, err, "read the pull requests gh listed: ")
}

func TestACommentGoesToGhThroughItsStandardInput(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	body := "Staging shows this pull request.\n\n- The app: https://app.203-0-113-10.sslip.io\n"

	err := fake.github().CommentOnPullRequest(context.Background(), 12, body)

	require.NoError(t, err)
	assert.Equal(t, []call{{Arguments: []string{"pr", "comment", "12", "--body-file", "-"}, Input: body}}, fake.calls())
}

func TestAFailureToCommentSaysOnWhichPullRequest(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(1, "", "GraphQL: Resource not accessible by integration (addComment)\n", "pr", "comment")

	err := fake.github().CommentOnPullRequest(context.Background(), 12, "Staging shows this pull request.")

	require.EqualError(t, err, "comment on the pull request #12: gh failed, with exit status 1: GraphQL: Resource not accessible by integration (addComment)")
}

// The label comes off through the REST API of issues, which the token of a
// job reaches with the permission to write pull requests.
func TestALabelComesOffThePullRequestThroughTheAPIOfItsIssue(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)

	err := fake.github().RemoveLabel(context.Background(), 7, "staging")

	require.NoError(t, err)
	assert.Equal(t, []call{{Arguments: []string{"api", "--method", "DELETE", "repos/{owner}/{repo}/issues/7/labels/staging", "--silent"}}}, fake.calls())
}

func TestAFailureToTakeALabelOffSaysOffWhichPullRequest(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(1, "", "gh: Resource not accessible by integration (HTTP 403)\n", "api", "--method", "DELETE")

	err := fake.github().RemoveLabel(context.Background(), 7, "staging")

	require.EqualError(t, err, "take the label staging off the pull request #7: gh failed, with exit status 1: gh: Resource not accessible by integration (HTTP 403)")
}

func TestAFailureOfGhSaysWhatFailedAndWhatGhSaidAndNoValue(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(4, "", "To get started with GitHub CLI, please run:  gh auth login\nAlternatively, populate the GH_TOKEN environment variable.\n", "secret", "set")

	err := fake.github().SetSecret(context.Background(), "staging", "DEPLOY_KEY", config.NewSecret(deployKey))

	require.EqualError(t, err, "set the secret DEPLOY_KEY of the GitHub Environment staging: gh failed, with exit status 4: "+
		"To get started with GitHub CLI, please run: gh auth login Alternatively, populate the GH_TOKEN environment variable.")
}

func TestAFailureToListTheEnvironmentsSaysSo(t *testing.T) {
	t.Parallel()
	fake := newFakeGh(t)
	fake.answers(1, "", "HTTP 403: Resource not accessible by personal access token\n", "api", environmentsList)

	err := fake.github().EnsureEnvironment(context.Background(), "staging")

	require.EqualError(t, err, "list the GitHub Environments: gh failed, with exit status 1: HTTP 403: Resource not accessible by personal access token")
}

func TestWithNoGhTheErrorSaysToInstallIt(t *testing.T) {
	t.Parallel()

	err := gh.New(gh.Settings{Executable: "gh-that-is-not-installed", Directory: t.TempDir()}).EnsureLabel(context.Background(), github.Label{Name: "staging"})

	require.EqualError(t, err, "create the label staging: the GitHub CLI, gh-that-is-not-installed, is not installed or not on the PATH: install it from https://cli.github.com and log in with gh auth login")
}
