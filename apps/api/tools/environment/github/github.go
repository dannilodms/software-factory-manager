// Package github names what the tool asks of the GitHub repository of the
// system: that a GitHub Environment and a label exist, a secret or a
// variable of an environment set or deleted, the open pull requests that
// have a label, a comment on a pull request, and a label taken off one. The
// adapter in the package gh calls the GitHub CLI, logged in as the
// maintainer on the machine and with the token of the job in the pipeline;
// githubtest keeps everything in memory for the tests of the commands.
package github

import (
	"context"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
)

// GitHub is the repository of the system on GitHub.
type GitHub interface {
	// EnsureEnvironment creates the GitHub Environment when it does not
	// exist, and changes nothing of one that does.
	EnsureEnvironment(ctx context.Context, environment string) error
	// EnsureLabel creates the label, or gives the one that exists its
	// description and its color.
	EnsureLabel(ctx context.Context, label Label) error
	// SetSecret creates or replaces a secret of the environment.
	SetSecret(ctx context.Context, environment, name string, value config.Secret) error
	// SetVariable creates or replaces a variable of the environment.
	SetVariable(ctx context.Context, environment, name, value string) error
	// DeleteSecret deletes a secret of the environment. Deleting one that
	// does not exist, or one of an environment that does not exist,
	// succeeds.
	DeleteSecret(ctx context.Context, environment, name string) error
	// DeleteVariable deletes a variable of the environment, as DeleteSecret
	// does a secret.
	DeleteVariable(ctx context.Context, environment, name string) error
	// LabelledPullRequests gives the open pull requests that have the
	// label.
	LabelledPullRequests(ctx context.Context, label string) ([]PullRequest, error)
	// CommentOnPullRequest adds a comment, written in Markdown, to the pull
	// request with that number.
	CommentOnPullRequest(ctx context.Context, number int, body string) error
	// RemoveLabel takes the label off the pull request with that number,
	// which has it.
	RemoveLabel(ctx context.Context, number int, label string) error
}

// Label is a label of the repository's pull requests.
type Label struct {
	Name        string
	Description string
	// Color is six hexadecimal digits, with no #.
	Color string
}

// PullRequest is an open pull request of the repository.
type PullRequest struct {
	Number int
	Title  string
	URL    string
}
