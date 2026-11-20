package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/database"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/database/databasetest"
	"github.com/dannilodms/software-factory-manager/apps/api/migrations"
)

// readyStandIn stands in for an API that is ready: it answers GET /readyz
// with 200, and anything else with 404. It returns the address it listens
// on, as host:port.
func readyStandIn(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server.Listener.Addr().String()
}

// portOf gives the port of a host:port address.
func portOf(t *testing.T, address string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	return port
}

// runProbe runs `probe-readiness` as a health check of Compose would,
// with the API_LISTEN_ADDRESS the API was given.
func runProbe(listenAddress string) (status int, stdout, stderr string) {
	var printed, logs output
	status = run(context.Background(), []string{"probe-readiness"}, map[string]string{"API_LISTEN_ADDRESS": listenAddress}, &printed, &logs)
	return status, printed.String(), logs.String()
}

func TestTheProbeOfReadinessPassesOnceTheAPIIsReady(t *testing.T) {
	t.Parallel()
	temporary := databasetest.New(t)
	_, err := database.Migrate(context.Background(), temporary.MigrationConnection(t), migrations.Files)
	require.NoError(t, err)
	api := startAPI(t, environmentOf(temporary))

	status, stdout, stderr := runProbe(api[len("http://"):])

	assert.Equal(t, 0, status, stderr)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestTheProbeOfReadinessFailsWhileAMigrationIsPending(t *testing.T) {
	t.Parallel()
	temporary := databasetest.New(t)
	api := startAPI(t, environmentOf(temporary))
	address := api[len("http://"):]

	status, stdout, stderr := runProbe(address)

	assert.Equal(t, 1, status)
	assert.Empty(t, stdout)
	assert.Equal(t, "api: the API at "+address+" is not ready: it answered 503\n", stderr)
}

func TestTheProbeOfReadinessFailsWhenNoAPIAnswers(t *testing.T) {
	t.Parallel()
	// A port that was free a moment ago, with nothing listening on it now.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	status, stdout, stderr := runProbe(address)

	assert.Equal(t, 1, status)
	assert.Empty(t, stdout)
	assert.Regexp(t, `^api: the API at `+address+` does not answer: .+\n$`, stderr)
}

func TestTheProbeOfReadinessReachesAnAPIThatListensOnEveryInterface(t *testing.T) {
	t.Parallel()
	port := portOf(t, readyStandIn(t))

	for _, listenAddress := range []string{"0.0.0.0:" + port, ":" + port, "[::]:" + port} {
		status, _, stderr := runProbe(listenAddress)

		assert.Equal(t, 0, status, "API_LISTEN_ADDRESS=%s: %s", listenAddress, stderr)
	}
}

func TestTheProbeOfReadinessNeedsTheListenAddressOfTheAPI(t *testing.T) {
	var stdout, stderr output

	status := run(context.Background(), []string{"probe-readiness"}, map[string]string{}, &stdout, &stderr)

	assert.Equal(t, 1, status)
	assert.Equal(t, "api: configuration: API_LISTEN_ADDRESS is required\n", stderr.String())
	assert.Empty(t, stdout.String())
}
