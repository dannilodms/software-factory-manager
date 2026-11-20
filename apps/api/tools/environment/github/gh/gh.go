// Package gh reaches GitHub through its command-line tool, gh, logged in on
// the machine as the maintainer, or with GH_TOKEN, the token of the job, in
// the pipeline. gh finds the repository from the remote of the checkout it
// runs in, or from GH_REPO.
//
// A value goes to gh through its standard input, never as an argument: an
// argument is seen by every process of the machine, and gh reads the value
// of a secret or a variable from its input when the input is not a
// terminal. gh takes the line breaks off the end of it.
package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
)

// Executable is the command of the GitHub CLI, found on the PATH.
const Executable = "gh"

// Settings are how the adapter runs gh.
type Settings struct {
	// Executable is the program run as gh.
	Executable string
	// Directory is where gh runs: a checkout of the repository.
	Directory string
}

// GitHub is the repository, reached through gh.
type GitHub struct {
	settings Settings
}

var _ github.GitHub = (*GitHub)(nil)

// New gives the repository that gh finds from the directory.
func New(settings Settings) *GitHub {
	return &GitHub{settings: settings}
}

// EnsureEnvironment lists the environments, and creates this one when it
// is not among them. An environment that exists is not written to, so that
// what was set on it by hand stays.
func (g *GitHub) EnsureEnvironment(ctx context.Context, environment string) error {
	exists, err := g.environmentExists(ctx, environment)
	if err != nil || exists {
		return err
	}
	_, err = g.run(ctx, "", "api", "--method", "PUT", "repos/{owner}/{repo}/environments/"+environment, "--silent")
	return failedTo("create the GitHub Environment "+environment, err)
}

// EnsureLabel creates the label, or changes the description and the color
// of the one that exists.
func (g *GitHub) EnsureLabel(ctx context.Context, label github.Label) error {
	_, err := g.run(ctx, "", "label", "create", label.Name, "--description", label.Description, "--color", label.Color, "--force")
	return failedTo("create the label "+label.Name, err)
}

// SetSecret gives gh the value on its standard input.
func (g *GitHub) SetSecret(ctx context.Context, environment, name string, value config.Secret) error {
	_, err := g.run(ctx, value.Reveal(), "secret", "set", name, "--env", environment)
	return failedTo(fmt.Sprintf("set the secret %s of the GitHub Environment %s", name, environment), err)
}

// SetVariable gives gh the value on its standard input too.
func (g *GitHub) SetVariable(ctx context.Context, environment, name, value string) error {
	_, err := g.run(ctx, value, "variable", "set", name, "--env", environment)
	return failedTo(fmt.Sprintf("set the variable %s of the GitHub Environment %s", name, environment), err)
}

// DeleteSecret deletes the secret when the environment lists it: gh fails
// to delete one that does not exist.
func (g *GitHub) DeleteSecret(ctx context.Context, environment, name string) error {
	return failedTo(fmt.Sprintf("delete the secret %s of the GitHub Environment %s", name, environment),
		g.delete(ctx, "secret", environment, name))
}

// DeleteVariable deletes the variable when the environment lists it.
func (g *GitHub) DeleteVariable(ctx context.Context, environment, name string) error {
	return failedTo(fmt.Sprintf("delete the variable %s of the GitHub Environment %s", name, environment),
		g.delete(ctx, "variable", environment, name))
}

// delete deletes a secret or a variable, whichever kind names, when it
// exists.
func (g *GitHub) delete(ctx context.Context, kind, environment, name string) error {
	exists, err := g.environmentExists(ctx, environment)
	if err != nil || !exists {
		return err
	}
	names, err := g.lines(ctx, kind, "list", "--env", environment, "--json", "name", "--jq", ".[].name")
	if err != nil || !containsName(names, name) {
		return err
	}
	_, err = g.run(ctx, "", kind, "delete", name, "--env", environment)
	return err
}

// listedPullRequest is an open pull request, as the list gh is asked for
// writes it: one object a line.
type listedPullRequest struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Labels []string `json:"labels"`
}

// LabelledPullRequests reads GitHub's own list of the open pull requests,
// page after page, and keeps the ones with the label. `gh pr list --label`
// would ask GitHub's search, which can still list a pull request whose label
// came off a moment ago, as the publish before took it off.
func (g *GitHub) LabelledPullRequests(ctx context.Context, label string) ([]github.PullRequest, error) {
	listed, err := g.run(ctx, "", "api", "repos/{owner}/{repo}/pulls?state=open&per_page=100", "--paginate",
		"--jq", ".[] | {number, title, url: .html_url, labels: [.labels[].name]}")
	if err != nil {
		return nil, err
	}
	var open []listedPullRequest
	decoder := json.NewDecoder(strings.NewReader(listed))
	for decoder.More() && err == nil {
		var pullRequest listedPullRequest
		err = decoder.Decode(&pullRequest)
		open = append(open, pullRequest)
	}
	if err != nil {
		return nil, fmt.Errorf("read the pull requests gh listed: %w", err)
	}
	withLabel := slices.DeleteFunc(open, func(pullRequest listedPullRequest) bool { return !containsName(pullRequest.Labels, label) })
	found := make([]github.PullRequest, 0, len(withLabel))
	for _, pullRequest := range withLabel {
		found = append(found, github.PullRequest{Number: pullRequest.Number, Title: pullRequest.Title, URL: pullRequest.URL})
	}
	return found, nil
}

// CommentOnPullRequest gives gh the body on its standard input too.
func (g *GitHub) CommentOnPullRequest(ctx context.Context, number int, body string) error {
	_, err := g.run(ctx, body, "pr", "comment", strconv.Itoa(number), "--body-file", "-")
	return failedTo(fmt.Sprintf("comment on the pull request #%d", number), err)
}

// RemoveLabel asks the REST API of issues, which serves pull requests too,
// to take the label off. Its documentation names the permission to write
// pull requests as enough for the token of a job.
func (g *GitHub) RemoveLabel(ctx context.Context, number int, label string) error {
	_, err := g.run(ctx, "", "api", "--method", "DELETE", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/labels/%s", number, label), "--silent")
	return failedTo(fmt.Sprintf("take the label %s off the pull request #%d", label, number), err)
}

// environmentExists tells whether the repository has the environment.
func (g *GitHub) environmentExists(ctx context.Context, environment string) (bool, error) {
	names, err := g.lines(ctx, "api", "repos/{owner}/{repo}/environments", "--paginate", "--jq", ".environments[]?.name")
	if err != nil {
		return false, fmt.Errorf("list the GitHub Environments: %w", err)
	}
	return containsName(names, environment), nil
}

// containsName tells whether the name is among the names, which GitHub
// compares regardless of case.
func containsName(names []string, name string) bool {
	return slices.ContainsFunc(names, func(listed string) bool { return strings.EqualFold(listed, name) })
}

// lines runs gh and gives the lines it printed that are not empty.
func (g *GitHub) lines(ctx context.Context, arguments ...string) ([]string, error) {
	printed, err := g.run(ctx, "", arguments...)
	if err != nil {
		return nil, err
	}
	var found []string
	for line := range strings.Lines(printed) {
		found = append(found, strings.TrimSpace(line))
	}
	return slices.DeleteFunc(found, func(line string) bool { return line == "" }), nil
}

// run runs gh with the input on its standard input, never a terminal, so
// that gh never asks anything, and gives what it printed. A failure says
// what gh said on its error output, which never holds the input.
func (g *GitHub) run(ctx context.Context, input string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, g.settings.Executable, arguments...)
	command.Dir = g.settings.Directory
	command.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("the GitHub CLI, %s, is not installed or not on the PATH: install it from https://cli.github.com and log in with gh auth login", g.settings.Executable)
	}
	if err != nil {
		return "", fmt.Errorf("gh failed, with %w: %s", err, strings.Join(strings.Fields(stderr.String()), " "))
	}
	return stdout.String(), nil
}

// failedTo names what failed, and is nil when nothing did.
func failedTo(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
