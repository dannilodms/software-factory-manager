package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/config"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/health"
	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/telemetry"
)

// probeReadiness asks the API that listens at API_LISTEN_ADDRESS whether it
// is ready, and succeeds only when readiness answers 200. It is the health
// check of the container of the API, whose image has no shell and no HTTP
// client to ask with. It prints nothing when the API is ready and one line
// when it is not (Health in CODING_STANDARDS.md).
func probeReadiness(ctx context.Context, environment map[string]string, _, stderr io.Writer) error {
	settings, err := config.Load(config.ProbeReadiness, environment)
	if err != nil {
		return err
	}
	telemetry.RouteTheStandardLogger(telemetry.NewLogger(stderr, settings.LogLevel))

	address := probedAddress(settings.ListenAddress)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+health.ReadinessPath, http.NoBody)
	if err != nil {
		return err
	}
	// Readiness answers within the timeout of its check, and the probe gives
	// the connection a second more. A transport of its own reads no proxy
	// from the environment.
	client := http.Client{Transport: &http.Transport{}, Timeout: settings.HealthCheckTimeout + time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("the API at %s does not answer: %w", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the API at %s is not ready: it answered %d", address, response.StatusCode)
	}
	return nil
}

// probedAddress is where the probe reaches the API: the address it listens
// on, with the loopback address in place of a host that names every
// interface, or none. The API listens on IPv4 there, wherever Docker runs.
func probedAddress(listen config.ListenAddress) string {
	// The configuration has checked that the address has a port.
	host, port, _ := net.SplitHostPort(listen.String())
	if host == "" || net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
