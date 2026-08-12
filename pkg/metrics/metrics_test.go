package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOperationalMetricsAreExposed(t *testing.T) {
	manager := NewMetricsManager("student-monitoring")
	manager.SetNATSConnected(true)
	manager.SetJobQueueDepth("monitoring", 3)
	manager.RecordBackgroundJob("academic_risk_calculation", "success", 250*time.Millisecond)
	manager.RecordDomainOperation("monitoring_overview", "success", 30*time.Millisecond)

	response := httptest.NewRecorder()
	manager.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{
		"nats_connected 1",
		`background_job_queue_depth{queue="monitoring"} 3`,
		`background_job_runs_total{job="academic_risk_calculation",outcome="success"} 1`,
		`domain_operations_total{operation="monitoring_overview",outcome="success"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics output does not contain %q", expected)
		}
	}
}
