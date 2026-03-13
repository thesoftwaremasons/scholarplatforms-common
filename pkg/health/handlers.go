package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// LivenessHandler returns an HTTP handler for liveness checks
// Returns 200 if the service is running (doesn't check dependencies)
func (hs *HealthService) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			hs.RecordRequest(time.Since(start))
		}()

		// Simple liveness check - just return OK if service is running
		response := map[string]interface{}{
			"status":       "healthy",
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
			"uptime":       time.Since(hs.startTime).String(),
			"service_name": hs.serviceName,
			"version":      hs.version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

// ReadinessHandler returns an HTTP handler for readiness checks
// Returns 200 if service is ready to handle traffic (checks dependencies)
func (hs *HealthService) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			hs.RecordRequest(time.Since(start))
		}()

		ctx := r.Context()
		report := hs.Check(ctx)

		w.Header().Set("Content-Type", "application/json")

		// Determine readiness based on critical checks
		statusCode := http.StatusOK
		if report.Status == StatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		response := map[string]interface{}{
			"status":       string(report.Status),
			"timestamp":    report.Timestamp.Format(time.RFC3339),
			"uptime":       report.Uptime.String(),
			"service_name": report.ServiceName,
			"version":      report.Version,
			"checks":       formatChecks(report.Checks),
		}

		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
	}
}

// HealthHandler returns a comprehensive health check (includes metrics)
func (hs *HealthService) HealthHandler() http.HandlerFunc {
	return hs.HTTPHandler()
}

// formatChecks formats check results for JSON response
func formatChecks(checks []CheckResult) []map[string]interface{} {
	formatted := make([]map[string]interface{}, len(checks))
	for i, check := range checks {
		formatted[i] = map[string]interface{}{
			"name":      check.Name,
			"status":    string(check.Status),
			"message":   check.Message,
			"error":     check.Error,
			"duration":  check.Duration.String(),
			"timestamp": check.Timestamp.Format(time.RFC3339),
		}
		if check.Details != nil {
			formatted[i]["details"] = check.Details
		}
	}
	return formatted
}

// RegisterHandlers registers health check handlers on a ServeMux
func (hs *HealthService) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/health/live", hs.LivenessHandler())
	mux.HandleFunc("/health/ready", hs.ReadinessHandler())
	mux.HandleFunc("/health", hs.HealthHandler())
}
