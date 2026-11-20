package versions_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/tools/guard/guardtest"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/guard/report"
	"github.com/dannilodms/software-factory-manager/apps/api/tools/guard/versions"
)

// check judges a tree with one file and returns what the check found.
func check(t *testing.T, file, content string) []report.Finding {
	t.Helper()
	findings, err := versions.Check(guardtest.Tree(t, map[string]string{file: content}))
	require.NoError(t, err)
	return findings
}

// kind is one kind of file that holds versions. Its template has one `%s`
// where a version is written, on the line `line`.
type kind struct {
	file     string
	template string
	line     int
	// exact are versions the check accepts, and inexact the ones it rejects:
	// a range, `latest` and a floating tag, as the kind of file writes them.
	exact   []string
	inexact []string
}

var kinds = map[string]kind{
	"the Go release of a module file": {
		file:     "apps/api/go.mod",
		template: "module example.test/api\n\ngo %s\n",
		line:     3,
		exact:    []string{"1.27.4"},
		inexact:  []string{"1.27", "1", "latest"},
	},
	"the toolchain of a module file": {
		file:     "apps/api/go.mod",
		template: "module example.test/api\n\ngo 1.27.4\n\ntoolchain %s\n",
		line:     5,
		exact:    []string{"go1.27.4"},
		inexact:  []string{"go1.27", "default", "latest"},
	},
	"a library of a module file": {
		file:     "apps/api/go.mod",
		template: "module example.test/api\n\ngo 1.27.4\n\nrequire (\n\tgithub.com/jackc/pgx/v5 %s\n)\n",
		line:     6,
		exact:    []string{"v5.13.0", "v0.0.0-20240606120523-5a60cdf6a761 // indirect", "v2.0.0+incompatible", "v4.0.0-rc.9"},
		inexact:  []string{"latest", "v5", "v5.13", ">=v5.13.0", "master"},
	},
	"a tool of the module file of the linter": {
		file:     "apps/api/golangci-lint.mod",
		template: "module example.test/api/golangci-lint\n\ngo 1.27.4\n\ntool github.com/golangci/golangci-lint/v2/cmd/golangci-lint\n\nrequire github.com/golangci/golangci-lint/v2 %s // indirect\n",
		line:     7,
		exact:    []string{"v2.15.0"},
		inexact:  []string{"latest", "v2", "upgrade"},
	},
	"an image of a Compose file": {
		file:     "deploy/compose.base.yaml",
		template: "services:\n  caddy:\n    image: %s\n",
		line:     3,
		exact: []string{
			"caddy:2.10.2",
			"caddy:2.10.2-alpine",
			"dxflrs/garage:v2.1.0",
			"postgres:18.7-trixie",
			"postgres:18.7",
			"ghcr.io/dexidp/dex:v2.44.0",
			"localhost:5000/caddy:2.10.2",
			"gcr.io/distroless/static-debian12@sha256:" + strings.Repeat("a", 64),
			"caddy:2@sha256:" + strings.Repeat("0", 64),
			// An image this repository builds and publishes, named on a
			// server by the commit it was built from.
			"ghcr.io/dannilodms/software-factory-manager-api:${COMMIT}",
			"ghcr.io/dannilodms/software-factory-manager-caddy:${COMMIT:?Set COMMIT, the hash of the commit published}",
			"ghcr.io/dannilodms/software-factory-manager-seed:0.1.0",
		},
		inexact: []string{
			"caddy",
			"caddy:latest",
			"caddy:2",
			"caddy:2.10",
			"caddy:alpine",
			"postgres:18",
			"postgres:18-trixie",
			"postgres:latest",
			"localhost:5000/caddy",
			"caddy@sha256:abc",
			"caddy:${CADDY_VERSION}",
			"ghcr.io/dannilodms/software-factory-manager-api",
			"ghcr.io/dannilodms/software-factory-manager-api:latest",
			"ghcr.io/dannilodms/software-factory-manager-api:v${COMMIT}",
			"ghcr.io/dannilodms/software-factory-manager-api:${COMMIT}-alpine",
			"ghcr.io/someone-else/software-factory-manager-api:${COMMIT}",
		},
	},
	"an image of the Compose file of development": {
		file:     "compose.yaml",
		template: "x-garage: &garage\n  image: %s\nservices:\n  garage: *garage\n",
		line:     2,
		exact:    []string{"dxflrs/garage:v2.1.0"},
		inexact:  []string{"dxflrs/garage:latest", "dxflrs/garage:v2"},
	},
	"the base image of a Dockerfile": {
		file:     "deploy/caddy/Dockerfile",
		template: "FROM --platform=$BUILDPLATFORM %s AS builder\nRUN xcaddy build\n",
		line:     1,
		exact:    []string{"caddy:2.10.2-builder", "scratch"},
		inexact:  []string{"caddy:builder", "caddy:latest", "caddy:2-builder", "caddy"},
	},
	"a plugin xcaddy builds into Caddy": {
		file:     "deploy/caddy/Dockerfile",
		template: "FROM caddy:2.11.9-builder-alpine AS builder\nRUN xcaddy build \\\n    --with %s \\\n    --with github.com/caddy-dns/cloudflare@v0.2.1\n",
		line:     3,
		exact: []string{
			"github.com/mholt/caddy-ratelimit@v0.1.0",
			"github.com/mholt/caddy-ratelimit@v0.1.1-0.20260612195517-5625512f24f6",
		},
		inexact: []string{
			"github.com/mholt/caddy-ratelimit",
			"github.com/mholt/caddy-ratelimit@latest",
			"github.com/mholt/caddy-ratelimit@master",
			"github.com/mholt/caddy-ratelimit@v0.1",
		},
	},
	"the Caddy xcaddy builds": {
		file:     "deploy/caddy/Dockerfile",
		template: "FROM caddy:2.11.9-builder-alpine AS builder\nRUN xcaddy build %s --with github.com/mholt/caddy-ratelimit@v0.1.0\n",
		line:     2,
		exact:    []string{"v2.11.9"},
		inexact:  []string{"latest", "master", "v2.11"},
	},
	"an image a Dockerfile copies from": {
		file:     "deploy/postgres.Dockerfile",
		template: "FROM postgres:18.7-trixie AS base\nFROM base\nCOPY --from=%s /usr/bin/pgbackrest /usr/bin/pgbackrest\n",
		line:     3,
		exact:    []string{"base", "0", "pgbackrest/pgbackrest:2.57.0"},
		inexact:  []string{"pgbackrest/pgbackrest:latest", "pgbackrest/pgbackrest:2"},
	},
	"an action of a workflow": {
		file:     ".github/workflows/ci.yml",
		template: "jobs:\n  backend:\n    runs-on: ubuntu-24.04\n    steps:\n      - uses: %s\n",
		line:     5,
		exact: []string{
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
			"./.github/actions/setup",
			"docker://alpine:3.22.1",
		},
		inexact: []string{
			"actions/checkout@v7",
			"actions/checkout@v7.0.1",
			"actions/checkout@main",
			"actions/checkout@3d3c42e",
			"actions/checkout",
			"docker://alpine:latest",
		},
	},
	"a tool a workflow installs": {
		file:     ".github/workflows/ci.yml",
		template: "env:\n  TASK_VERSION: %s\n  TASK_SHA256: 680859dbb4d881a9c72d4d9a8f510825450849af8567deacd7302c01124416fb\n",
		line:     2,
		exact:    []string{"3.54.0", "v3.54.0", `"3.54.0"`},
		inexact:  []string{"3.54", "3", "latest", "3.x", `"^3.54.0"`},
	},
	"a tool a Task target runs": {
		file:     "Taskfile.yml",
		template: "version: \"3\"\n\ntasks:\n  workflows:lint:\n    vars:\n      ACTIONLINT_VERSION: %s\n",
		line:     6,
		exact:    []string{"1.7.12", `"1.7.12"`},
		inexact:  []string{"1.7", "1", "latest", `"^1.7.12"`},
	},
	"an image a Task target runs": {
		file:     "Taskfile.yml",
		template: "version: \"3\"\n\ntasks:\n  workflows:check:\n    vars:\n      ACTIONLINT_IMAGE: %s\n",
		line:     6,
		exact: []string{
			"rhysd/actionlint:1.7.12@sha256:" + strings.Repeat("b", 64),
			"ghcr.io/zizmorcore/zizmor:1.32.0@sha256:" + strings.Repeat("3", 64),
			"rhysd/actionlint:1.7.12",
		},
		inexact: []string{
			"rhysd/actionlint",
			"rhysd/actionlint:latest",
			"rhysd/actionlint:1.7",
			"rhysd/actionlint@sha256:abc",
		},
	},
	"a tool an action installs": {
		file:     ".github/workflows/ci.yml",
		template: "jobs:\n  app:\n    steps:\n      - uses: ./.github/actions/setup\n        with:\n          node-version: %s\n          go-version-file: apps/api/go.mod\n",
		line:     6,
		exact:    []string{"26.13.1"},
		inexact:  []string{"26", "26.x", "lts/*", "latest"},
	},
	"the system of a runner": {
		file:     ".github/workflows/ci.yml",
		template: "jobs:\n  backend:\n    runs-on: %s\n",
		line:     3,
		exact:    []string{"ubuntu-24.04", "[self-hosted, linux]"},
		inexact:  []string{"ubuntu-latest", "[ubuntu-latest]"},
	},
	"the image of a job of a workflow": {
		file:     ".github/workflows/ci.yml",
		template: "jobs:\n  backend:\n    services:\n      postgres:\n        image: %s\n",
		line:     5,
		exact:    []string{"postgres:18.7-trixie"},
		inexact:  []string{"postgres:18", "postgres:latest"},
	},
	"a package of the catalog": {
		file:     "apps/web/pnpm-workspace.yaml",
		template: "catalog:\n  react: %s\n",
		line:     2,
		exact:    []string{"19.3.1", "0.6.0-beta.3", `"19.3.1"`},
		inexact:  []string{"^19.3.1", "~19.3.1", "19.3", "19", "19.x", `"*"`, "latest", "next", `">=19.3.1"`, "19.3.1 || 20.0.0"},
	},
	"a package of a named catalog": {
		file:     "apps/web/pnpm-workspace.yaml",
		template: "catalogs:\n  react18:\n    react: %s\n",
		line:     3,
		exact:    []string{"18.3.1"},
		inexact:  []string{"^18.3.1", "latest"},
	},
	"an override of the catalog": {
		file:     "apps/web/pnpm-workspace.yaml",
		template: "overrides:\n  \"vite@*\": %s\n",
		line:     2,
		exact:    []string{"npm:@voidzero-dev/vite-plus-core@1.6.0", "5.1.0", `"catalog:"`},
		inexact:  []string{"npm:@voidzero-dev/vite-plus-core@latest", "npm:@voidzero-dev/vite-plus-core", "^5.1.0"},
	},
	"a dependency of the package manifest": {
		file:     "apps/web/package.json",
		template: "{\n  \"dependencies\": {\n    \"react\": \"%s\"\n  }\n}\n",
		line:     3,
		exact:    []string{"catalog:", "catalog:react18", "19.3.1"},
		inexact:  []string{"^19.3.1", "latest", "*", "19"},
	},
	"the runtime of the package manifest": {
		file:     "apps/web/package.json",
		template: "{\n  \"devEngines\": {\n    \"runtime\": {\n      \"name\": \"node\",\n      \"version\": \"%s\"\n    }\n  }\n}\n",
		line:     5,
		exact:    []string{"26.13.1"},
		inexact:  []string{"26", ">=26.13.1", "^26.13.1", "lts"},
	},
	"the package manager of the package manifest": {
		file:     "apps/web/package.json",
		template: "{\n  \"packageManager\": \"%s\"\n}\n",
		line:     2,
		exact:    []string{"pnpm@12.21.0", "pnpm@12.21.0+sha512.abc"},
		inexact:  []string{"pnpm@12", "pnpm@latest", "pnpm"},
	},
	"an MCP server run by npx": {
		file:     ".mcp.json",
		template: "{\n  \"mcpServers\": {\n    \"chrome-devtools\": {\n      \"command\": \"cmd\",\n      \"args\": [\"/c\", \"npx\", \"-y\", \"%s\", \"--no-usage-statistics\"]\n    }\n  }\n}\n",
		line:     5,
		exact:    []string{"chrome-devtools-mcp@1.12.0", "@playwright/mcp@0.0.41"},
		inexact:  []string{"chrome-devtools-mcp", "chrome-devtools-mcp@latest", "chrome-devtools-mcp@1", "chrome-devtools-mcp@^1.12.0", "@playwright/mcp"},
	},
	"an MCP server run by pnpm dlx": {
		file:     ".mcp.json",
		template: "{\n  \"mcpServers\": {\n    \"context7\": {\n      \"command\": \"pnpm\",\n      \"args\": [\"dlx\", \"%s\"]\n    }\n  }\n}\n",
		line:     5,
		exact:    []string{"@upstash/context7-mcp@2.1.0"},
		inexact:  []string{"@upstash/context7-mcp@latest", "@upstash/context7-mcp"},
	},
}

func TestAVersionThatIsNotExactIsRejected(t *testing.T) {
	t.Parallel()
	for name, kind := range kinds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, version := range kind.inexact {
				t.Run(version, func(t *testing.T) {
					t.Parallel()

					findings := check(t, kind.file, strings.Replace(kind.template, "%s", version, 1))

					require.Len(t, findings, 1)
					assert.Equal(t, kind.file, findings[0].File)
					assert.Equal(t, kind.line, findings[0].Line)
					assert.Contains(t, findings[0].Message, "(Versions in CODING_STANDARDS.md)")
				})
			}
		})
	}
}

func TestAnExactVersionPasses(t *testing.T) {
	t.Parallel()
	for name, kind := range kinds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, version := range kind.exact {
				t.Run(version, func(t *testing.T) {
					t.Parallel()

					findings := check(t, kind.file, strings.Replace(kind.template, "%s", version, 1))

					assert.Empty(t, findings)
				})
			}
		})
	}
}

func TestAFloatingImageTagInAComposeFileIsRejected(t *testing.T) {
	t.Parallel()

	findings := check(t, "deploy/compose.base.yaml", `services:
  postgres:
    image: postgres:18
`)

	assert.Equal(t, []report.Finding{{
		File:    "deploy/compose.base.yaml",
		Line:    3,
		Message: `image "postgres:18" names no exact version: write the full version in its tag, or pin the image by its digest (Versions in CODING_STANDARDS.md)`,
	}}, findings)
}

func TestARangeInTheCatalogIsNamed(t *testing.T) {
	t.Parallel()

	findings := check(t, "apps/web/pnpm-workspace.yaml", "catalog:\n  react: ^19.3.1\n")

	assert.Equal(t, []report.Finding{{
		File:    "apps/web/pnpm-workspace.yaml",
		Line:    2,
		Message: `react is "^19.3.1", not an exact version: write one version, with no range and no tag (Versions in CODING_STANDARDS.md)`,
	}}, findings)
}

func TestAServiceBuiltFromADockerfileNamesItsOwnImage(t *testing.T) {
	t.Parallel()

	findings := check(t, "compose.yaml", `services:
  caddy:
    build: deploy/caddy
    image: software-factory-manager/caddy
`)

	assert.Empty(t, findings)
}

func TestAnMCPServerThatDownloadsNothingHoldsNoVersion(t *testing.T) {
	t.Parallel()

	findings := check(t, ".mcp.json", `{
  "mcpServers": {
    "remote": { "type": "http", "url": "https://mcp.example.test/mcp" },
    "local": { "command": "node", "args": ["./scripts/server.mjs"] }
  }
}
`)

	assert.Empty(t, findings)
}

func TestAnMCPServerThatNamesNoPackageIsRejected(t *testing.T) {
	t.Parallel()

	findings := check(t, ".mcp.json", `{
  "mcpServers": {
    "broken": { "command": "npx", "args": ["-y"] }
  }
}
`)

	require.Len(t, findings, 1)
	assert.Equal(t, 3, findings[0].Line)
}

func TestWhatIsInstalledAndTheFixturesAreNotRead(t *testing.T) {
	t.Parallel()
	floating := "catalog:\n  react: latest\n"
	root := guardtest.Tree(t, map[string]string{
		"apps/web/node_modules/react/package.json":    `{"dependencies": {"loose-envify": "^1.1.0"}}`,
		"apps/api/tools/guard/testdata/compose.yaml":  "services:\n  postgres:\n    image: postgres\n",
		".claude/worktrees/other/pnpm-workspace.yaml": floating,
		"docs/notes.yaml": "image: postgres:latest\n",
		"apps/web/src/consultancy/locales/en/app.json": `{"version": "latest"}`,
	})

	findings, err := versions.Check(root)

	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestEveryFindingOfARepositoryIsListedInOrder(t *testing.T) {
	t.Parallel()
	root := guardtest.Tree(t, map[string]string{
		"deploy/compose.base.yaml": "services:\n  postgres:\n    image: postgres:18\n  caddy:\n    image: caddy:latest\n",
		".mcp.json":                `{"mcpServers": {"a": {"command": "npx", "args": ["a-mcp"]}}}`,
		"apps/api/go.mod":          "module example.test/api\n\ngo 1.27\n",
	})

	findings, err := versions.Check(root)

	require.NoError(t, err)
	require.Len(t, findings, 4)
	assert.Equal(t, ".mcp.json", findings[0].File)
	assert.Equal(t, "apps/api/go.mod", findings[1].File)
	assert.Equal(t, "deploy/compose.base.yaml", findings[2].File)
	assert.Equal(t, 3, findings[2].Line)
	assert.Equal(t, 5, findings[3].Line)
}

func TestAFileThatDoesNotParseIsAnError(t *testing.T) {
	t.Parallel()
	root := guardtest.Tree(t, map[string]string{"compose.yaml": "services: [\n"})

	_, err := versions.Check(root)

	require.ErrorContains(t, err, "compose.yaml")
}
