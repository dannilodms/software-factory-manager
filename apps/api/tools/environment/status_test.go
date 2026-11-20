package main

import (
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/github"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/environment/provider"
)

func TestTheStatusOfStagingWhenOff(t *testing.T) {
	t.Parallel()
	world := aWorld(t)

	printed := world.succeeds("status", "staging")

	assert.Equal(t, "Staging is off.\nNo open pull request has the staging label.\n", printed)
}

func TestTheStatusOfStagingWhenOnTellsSinceWhenAndAtWhichAddressAndHostNames(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.clock.now = world.clock.now.Add(26*time.Hour + 5*time.Minute + 30*time.Second)

	printed := world.succeeds("status", "staging")

	assert.Equal(t, `Staging is on, since 2026-11-19 14:00 UTC (26h05m ago), at 203.0.113.10.
No open pull request has the staging label.
  App:    https://app.203-0-113-10.sslip.io
  Files:  https://files.203-0-113-10.sslip.io
  Mail:   https://mail.203-0-113-10.sslip.io
  SSH:    ssh -F "`+filepath.ToSlash(filepath.Join(world.localCopy(), "ssh_config"))+`" staging
`, printed)
}

func TestTheStatusOfAStagingTurnedOnFromAnotherMachineSaysThisOneHasNoKey(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	created := time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC)
	world.provider.Add(provider.Server{Name: "staging", Address: netip.MustParseAddr("198.51.100.7"), Created: created})

	printed := world.succeeds("status", "staging")

	assert.Equal(t, `Staging is on, since 2026-11-19 09:30 UTC (4h30m ago), at 198.51.100.7.
No open pull request has the staging label.
  App:    https://app.198-51-100-7.sslip.io
  Files:  https://files.198-51-100-7.sslip.io
  Mail:   https://mail.198-51-100-7.sslip.io
  SSH:    the main checkout keeps no copy of the deploy key
`, printed)
}

func TestTheStatusNamesTheOpenPullRequestWithTheStagingLabel(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.succeeds("on", "staging")
	world.github.AddPullRequest(github.PullRequest{Number: 11, Title: "Another change", URL: "https://github.com/dannilodms/software-factory-manager/pull/11"}, "documentation")
	world.github.AddPullRequest(github.PullRequest{Number: 12, Title: "Publish to staging", URL: "https://github.com/dannilodms/software-factory-manager/pull/12"}, "staging")

	printed := world.succeeds("status", "staging")

	assert.Equal(t, `Staging is on, since 2026-11-19 14:00 UTC (0h00m ago), at 203.0.113.10.
Pull request #12 has the staging label: Publish to staging, https://github.com/dannilodms/software-factory-manager/pull/12
  App:    https://app.203-0-113-10.sslip.io
  Files:  https://files.203-0-113-10.sslip.io
  Mail:   https://mail.203-0-113-10.sslip.io
  SSH:    ssh -F "`+filepath.ToSlash(filepath.Join(world.localCopy(), "ssh_config"))+`" staging
`, printed)
}

func TestTheStatusWhenOffStillNamesAPullRequestWithTheLabel(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.AddPullRequest(github.PullRequest{Number: 12, Title: "Publish to staging", URL: "https://github.com/dannilodms/software-factory-manager/pull/12"}, "staging")
	world.github.AddPullRequest(github.PullRequest{Number: 13, Title: "Also labelled", URL: "https://github.com/dannilodms/software-factory-manager/pull/13"}, "staging", "documentation")

	printed := world.succeeds("status", "staging")

	assert.Equal(t, `Staging is off.
Pull request #12 has the staging label: Publish to staging, https://github.com/dannilodms/software-factory-manager/pull/12
Pull request #13 has the staging label: Also labelled, https://github.com/dannilodms/software-factory-manager/pull/13
`, printed)
}

func TestAStatusThatCannotReachGitHubSaysSoAfterWhatItKnows(t *testing.T) {
	t.Parallel()
	world := aWorld(t)
	world.github.Unreachable(errors.New("gh is not logged in"))

	ran := world.run("status", "staging")

	assert.Equal(t, 1, ran.status)
	assert.Equal(t, "Staging is off.\n", ran.stdout)
	assert.Equal(t, "environment: find the open pull requests with the label staging on GitHub: gh is not logged in\n", ran.stderr)
}
