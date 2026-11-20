// Package githubtest keeps a repository of GitHub in memory, for the tests
// of the commands of the tool.
package githubtest

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
)

// contents are what a GitHub Environment holds.
type contents struct {
	secrets   map[string]string
	variables map[string]string
}

// labelled is an open pull request and its labels.
type labelled struct {
	pullRequest github.PullRequest
	labels      []string
}

// GitHub is a repository whose environments, labels and pull requests live
// in memory. Like GitHub, it refuses a secret or a variable of an
// environment that does not exist.
type GitHub struct {
	environments map[string]contents
	labels       map[string]github.Label
	pullRequests []labelled
	comments     map[int][]string
	written      []string
	unreachable  error
	setErrs      map[string]error
	commentErrs  map[int]error
	removeErrs   map[int]error
}

// NewGitHub gives a repository with no environment, no label and no pull
// request.
func NewGitHub() *GitHub {
	return &GitHub{
		environments: map[string]contents{},
		labels:       map[string]github.Label{},
		comments:     map[int][]string{},
		setErrs:      map[string]error{},
		commentErrs:  map[int]error{},
		removeErrs:   map[int]error{},
	}
}

// FailCommentingOn makes every later comment on the pull request with that
// number fail with err and add nothing.
func (g *GitHub) FailCommentingOn(number int, err error) {
	g.commentErrs[number] = err
}

// FailRemovingALabelFrom makes every later removal of a label from the pull
// request with that number fail with err and remove nothing.
func (g *GitHub) FailRemovingALabelFrom(number int, err error) {
	g.removeErrs[number] = err
}

// Unreachable makes every later call fail with err and change nothing, as
// when gh is not logged in.
func (g *GitHub) Unreachable(err error) {
	g.unreachable = err
}

// FailSettingWith makes every later setting of the secret or the variable
// with that name fail with err and set nothing.
func (g *GitHub) FailSettingWith(name string, err error) {
	g.setErrs[name] = err
}

// AddPullRequest opens a pull request with those labels.
func (g *GitHub) AddPullRequest(pullRequest github.PullRequest, labels ...string) {
	g.pullRequests = append(g.pullRequests, labelled{pullRequest: pullRequest, labels: labels})
}

// Close closes the pull request with that number, which keeps its labels.
func (g *GitHub) Close(number int) {
	g.pullRequests = slices.DeleteFunc(g.pullRequests, func(open labelled) bool { return open.pullRequest.Number == number })
}

// EnsureEnvironment creates the environment when it does not exist.
func (g *GitHub) EnsureEnvironment(_ context.Context, name string) error {
	if g.unreachable != nil {
		return g.unreachable
	}
	if _, exists := g.environments[name]; exists {
		return nil
	}
	g.environments[name] = contents{secrets: map[string]string{}, variables: map[string]string{}}
	return nil
}

// EnsureLabel creates or replaces the label.
func (g *GitHub) EnsureLabel(_ context.Context, label github.Label) error {
	if g.unreachable != nil {
		return g.unreachable
	}
	g.written = append(g.written, label.Name, label.Description, label.Color)
	g.labels[label.Name] = label
	return nil
}

// SetSecret keeps the value of the secret.
func (g *GitHub) SetSecret(_ context.Context, environment, name string, value config.Secret) error {
	return g.set(environment, name, value.Reveal(), func(e contents) map[string]string { return e.secrets })
}

// SetVariable keeps the value of the variable.
func (g *GitHub) SetVariable(_ context.Context, environment, name, value string) error {
	return g.set(environment, name, value, func(e contents) map[string]string { return e.variables })
}

func (g *GitHub) set(name, key, value string, values func(contents) map[string]string) error {
	if g.unreachable != nil {
		return g.unreachable
	}
	if err := g.setErrs[key]; err != nil {
		return err
	}
	found, exists := g.environments[name]
	if !exists {
		return fmt.Errorf("the environment %s does not exist", name)
	}
	g.written = append(g.written, key, value)
	values(found)[key] = value
	return nil
}

// DeleteSecret deletes the secret, if there is one.
func (g *GitHub) DeleteSecret(_ context.Context, environment, name string) error {
	return g.delete(environment, name, func(e contents) map[string]string { return e.secrets })
}

// DeleteVariable deletes the variable, if there is one.
func (g *GitHub) DeleteVariable(_ context.Context, environment, name string) error {
	return g.delete(environment, name, func(e contents) map[string]string { return e.variables })
}

func (g *GitHub) delete(name, key string, values func(contents) map[string]string) error {
	if g.unreachable != nil {
		return g.unreachable
	}
	found, exists := g.environments[name]
	if !exists {
		return nil
	}
	delete(values(found), key)
	return nil
}

// LabelledPullRequests gives the open pull requests with the label, in the
// order they were opened.
func (g *GitHub) LabelledPullRequests(_ context.Context, label string) ([]github.PullRequest, error) {
	if g.unreachable != nil {
		return nil, g.unreachable
	}
	withLabel := slices.DeleteFunc(slices.Clone(g.pullRequests), func(open labelled) bool {
		return !slices.Contains(open.labels, label)
	})
	found := make([]github.PullRequest, 0, len(withLabel))
	for _, open := range withLabel {
		found = append(found, open.pullRequest)
	}
	return found, nil
}

// CommentOnPullRequest keeps the comment.
func (g *GitHub) CommentOnPullRequest(_ context.Context, number int, body string) error {
	if g.unreachable != nil {
		return g.unreachable
	}
	if err := g.commentErrs[number]; err != nil {
		return err
	}
	g.written = append(g.written, body)
	g.comments[number] = append(g.comments[number], body)
	return nil
}

// RemoveLabel takes the label off the pull request. Like GitHub, it refuses
// to take off a label the pull request does not have.
func (g *GitHub) RemoveLabel(_ context.Context, number int, label string) error {
	if g.unreachable != nil {
		return g.unreachable
	}
	if err := g.removeErrs[number]; err != nil {
		return err
	}
	found := slices.IndexFunc(g.pullRequests, func(open labelled) bool {
		return open.pullRequest.Number == number && slices.Contains(open.labels, label)
	})
	if found < 0 {
		return fmt.Errorf("the pull request #%d does not have the label %s", number, label)
	}
	g.pullRequests[found].labels = slices.DeleteFunc(g.pullRequests[found].labels, func(name string) bool { return name == label })
	return nil
}

// Comments gives the comments of the pull request, in the order they were
// made.
func (g *GitHub) Comments(number int) []string {
	return slices.Clone(g.comments[number])
}

// LabelsOf gives the labels of the pull request with that number.
func (g *GitHub) LabelsOf(number int) []string {
	found := slices.IndexFunc(g.pullRequests, func(open labelled) bool { return open.pullRequest.Number == number })
	if found < 0 {
		return nil
	}
	return slices.Clone(g.pullRequests[found].labels)
}

// Environments gives the names of the environments that exist.
func (g *GitHub) Environments() []string {
	return slices.Sorted(maps.Keys(g.environments))
}

// Secrets gives the secrets of the environment, by name.
func (g *GitHub) Secrets(name string) map[string]string {
	return maps.Clone(g.environments[name].secrets)
}

// Variables gives the variables of the environment, by name.
func (g *GitHub) Variables(name string) map[string]string {
	return maps.Clone(g.environments[name].variables)
}

// Labels gives the labels that exist, by name.
func (g *GitHub) Labels() map[string]github.Label {
	return maps.Clone(g.labels)
}

// Written gives every name and value the repository was given, in order,
// secrets included.
func (g *GitHub) Written() []string {
	return slices.Clone(g.written)
}

// Clear deletes every secret and variable of the environment, as the
// maintainer can in the settings of the repository.
func (g *GitHub) Clear(name string) {
	clear(g.environments[name].secrets)
	clear(g.environments[name].variables)
}
