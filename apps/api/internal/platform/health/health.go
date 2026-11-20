// Package health answers liveness and readiness. Both live outside /api and
// outside the OpenAPI spec, need no authentication and answer with the status
// code alone. Rules: Health in CODING_STANDARDS.md.
package health

import (
	"context"
	"net/http"
	"time"

	"github.com/dannilodms/software-factory-manager/apps/api/internal/platform/telemetry"
)

// Check tells whether the essential parts answer. A nil error means ready.
type Check func(ctx context.Context) error

const (
	livenessPath = "/livez"
	// ReadinessPath is where readiness answers. The probe of the container
	// of the API asks it there.
	ReadinessPath = "/readyz"
)

// Paths gives the paths of liveness and readiness. A probe calls them every
// few seconds, so the telemetry of a request records no span for them.
func Paths() []string {
	return []string{livenessPath, ReadinessPath}
}

// Mount adds GET /livez and GET /readyz to the mux. Liveness touches no part.
// Readiness runs the check under the timeout and answers 503 when it fails or
// does not end in time. The raw error of the check goes to the logs only.
func Mount(mux *http.ServeMux, ready Check, timeout time.Duration) {
	mux.HandleFunc("GET "+livenessPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET "+ReadinessPath, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := ready(ctx); err != nil {
			telemetry.RecordFailure(r.Context(), err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
