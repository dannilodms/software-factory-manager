package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wait for /up asks a server of the test over HTTPS, as it asks the app
// of a server through Caddy.

// anApp answers /up with the statuses given, one for each request, then
// with the last one.
func anApp(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var asked atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count := int(asked.Add(1))
		if request.URL.Path != "/up" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.WriteHeader(statuses[min(count, len(statuses))-1])
	}))
	// A client that does not trust the certificate fails the handshake, which
	// the server would log.
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, &asked
}

func waitingFor(server *httptest.Server) upOverHTTPS {
	return upOverHTTPS{client: server.Client(), interval: 10 * time.Millisecond}
}

func TestTheWaitEndsOnceUpAnswersOK(t *testing.T) {
	t.Parallel()
	server, asked := anApp(t, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK)

	err := waitingFor(server).WaitUntilUp(context.Background(), server.URL+"/up")

	require.NoError(t, err)
	assert.Equal(t, int32(3), asked.Load(), "it asks again until the app answers")
}

func TestAnAppThatNeverAnswersOKIsGivenUpWithTheLastAnswer(t *testing.T) {
	t.Parallel()
	server, _ := anApp(t, http.StatusBadGateway)
	// Long enough for a first answer on a machine under load.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := waitingFor(server).WaitUntilUp(ctx, server.URL+"/up")

	require.EqualError(t, err, server.URL+"/up did not answer 200 in time: it answered 502 Bad Gateway")
}

func TestACertificateThatIsNotTrustedIsNotAnAnswer(t *testing.T) {
	t.Parallel()
	server, asked := anApp(t, http.StatusOK)
	// Long enough for a first handshake on a machine under load.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	untrusting := upOverHTTPS{client: &http.Client{}, interval: 10 * time.Millisecond}

	err := untrusting.WaitUntilUp(ctx, server.URL+"/up")

	require.ErrorContains(t, err, "did not answer 200 in time")
	require.ErrorContains(t, err, "certificate")
	assert.Zero(t, asked.Load(), "no request reaches an app whose certificate is not trusted")
}
