// Package testing provides example integration tests
package testing

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestServiceIntegration demonstrates how to write comprehensive integration tests
func TestServiceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Initialize the integration test suite
	suite, err := NewIntegrationTestSuite(t, "./test_config.yaml")
	if err != nil {
		t.Fatalf("Failed to create integration test suite: %v", err)
	}
	defer suite.Cleanup()

	ctx := context.Background()

	// Load test configuration
	var testConfig TestConfig
	if err := suite.setupTestEnvironment(ctx, &testConfig); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}

	t.Run("Infrastructure Setup", func(t *testing.T) {
		testInfrastructureSetup(t, suite, testConfig)
	})

	t.Run("Service Health Checks", func(t *testing.T) {
		testServiceHealthChecks(t, suite)
	})

	t.Run("API Gateway Routing", func(t *testing.T) {
		testAPIGatewayRouting(t, suite)
	})

	t.Run("Service Communication", func(t *testing.T) {
		testServiceCommunication(t, suite)
	})

	t.Run("Database Operations", func(t *testing.T) {
		testDatabaseOperations(t, suite)
	})

	t.Run("End-to-End Workflows", func(t *testing.T) {
		testEndToEndWorkflows(t, suite)
	})

	t.Run("Performance Tests", func(t *testing.T) {
		testPerformance(t, suite)
	})
}

// testInfrastructureSetup tests that all infrastructure components start correctly
func testInfrastructureSetup(t *testing.T, suite *IntegrationTestSuite, config TestConfig) {
	ctx := context.Background()

	// Setup infrastructure (CockroachDB, Redis, NATS)
	if err := suite.SetupInfrastructure(config); err != nil {
		t.Fatalf("Failed to setup infrastructure: %v", err)
	}

	// Verify database connection
	db, err := suite.GetDatabase("cockroachdb")
	if err != nil {
		t.Fatalf("Failed to get database connection: %v", err)
	}

	if err := db.PingContext(ctx); err != nil {
		t.Errorf("Database ping failed: %v", err)
	}

	// Setup database schema and test data
	dbUtils := NewDatabaseTestUtils(db, suite.logger, "./testdata")
	if err := dbUtils.SetupTestSchema(ctx); err != nil {
		t.Errorf("Failed to setup test schema: %v", err)
	}

	// Load test fixtures
	if err := dbUtils.LoadFixtures(ctx, "users", "notifications", "user_profiles"); err != nil {
		t.Errorf("Failed to load test fixtures: %v", err)
	}
}

// testServiceHealthChecks tests that all services respond to health checks
func testServiceHealthChecks(t *testing.T, suite *IntegrationTestSuite) {
	services := []string{"apigateway", "notification", "userprofile", "zitadel-service"}

	// Wait for all services to be ready
	if err := suite.WaitForServices(2 * time.Minute); err != nil {
		t.Fatalf("Services did not become ready: %v", err)
	}

	for _, serviceName := range services {
		t.Run(serviceName, func(t *testing.T) {
			service, err := suite.GetService(serviceName)
			if err != nil {
				t.Fatalf("Failed to get service %s: %v", serviceName, err)
			}

			// Create API client for the service
			client := NewAPITestClient(service.BaseURL, suite.logger)

			// Test health endpoint
			resp, err := client.GET("/health")
			if err != nil {
				t.Errorf("Health check failed for %s: %v", serviceName, err)
				return
			}

			// Assert response
			if err := resp.AssertStatusCode(200); err != nil {
				t.Errorf("Health check assertion failed for %s: %v", serviceName, err)
			}

			// Assert response time
			if err := resp.AssertDuration(1 * time.Second); err != nil {
				t.Errorf("Health check too slow for %s: %v", serviceName, err)
			}
		})
	}
}

// testAPIGatewayRouting tests that API Gateway correctly routes requests
func testAPIGatewayRouting(t *testing.T, suite *IntegrationTestSuite) {
	gateway, err := suite.GetService("apigateway")
	if err != nil {
		t.Fatalf("Failed to get API Gateway service: %v", err)
	}

	client := NewAPITestClient(gateway.BaseURL, suite.logger)

	testCases := []struct {
		name     string
		endpoint string
		service  string
	}{
		{"Notification Service", "/api/notification/health", "notification"},
		{"User Profile Service", "/api/userprofile/health", "userprofile"},
		{"Zitadel Service", "/api/zitadel/health", "zitadel-service"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.GET(tc.endpoint)
			if err != nil {
				t.Errorf("Failed to route to %s: %v", tc.service, err)
				return
			}

			if err := resp.AssertStatusCode(200); err != nil {
				t.Errorf("Routing assertion failed for %s: %v", tc.service, err)
			}
		})
	}
}

// testServiceCommunication tests inter-service communication
func testServiceCommunication(t *testing.T, suite *IntegrationTestSuite) {
	gateway, err := suite.GetService("apigateway")
	if err != nil {
		t.Fatalf("Failed to get API Gateway service: %v", err)
	}

	client := NewAPITestClient(gateway.BaseURL, suite.logger)

	t.Run("Notification Creation", func(t *testing.T) {
		// Test creating a notification through the API Gateway
		notification := map[string]interface{}{
			"user_id": "test-user-123",
			"type":    "email",
			"subject": "Test Notification",
			"content": "This is a test notification",
		}

		resp, err := client.POST("/api/notification/notifications", notification)
		if err != nil {
			t.Errorf("Failed to create notification: %v", err)
			return
		}

		if err := resp.AssertStatusCode(201); err != nil {
			t.Errorf("Notification creation failed: %v", err)
		}

		// Verify notification was created
		if err := resp.AssertJSONField("status", "created"); err != nil {
			t.Errorf("Notification creation response invalid: %v", err)
		}
	})

	t.Run("User Profile Update", func(t *testing.T) {
		// Test updating a user profile
		profile := map[string]interface{}{
			"user_id":    "test-user-123",
			"first_name": "Test",
			"last_name":  "User",
			"email":      "test@example.com",
		}

		resp, err := client.PUT("/api/userprofile/profiles/test-user-123", profile)
		if err != nil {
			t.Errorf("Failed to update user profile: %v", err)
			return
		}

		if err := resp.AssertStatusCode(200); err != nil {
			t.Errorf("User profile update failed: %v", err)
		}
	})
}

// testDatabaseOperations tests database operations and data consistency
func testDatabaseOperations(t *testing.T, suite *IntegrationTestSuite) {
	db, err := suite.GetDatabase("cockroachdb")
	if err != nil {
		t.Fatalf("Failed to get database connection: %v", err)
	}

	ctx := context.Background()
	dbUtils := NewDatabaseTestUtils(db, suite.logger, "./testdata")

	t.Run("Data Persistence", func(t *testing.T) {
		// Create a snapshot before operations
		snapshot, err := dbUtils.CreateSnapshot(ctx, "before_operations")
		if err != nil {
			t.Errorf("Failed to create database snapshot: %v", err)
		}

		// Perform some database operations through the API
		gateway, _ := suite.GetService("apigateway")
		client := NewAPITestClient(gateway.BaseURL, suite.logger)

		// Create a notification
		notification := map[string]interface{}{
			"user_id": "test-user-456",
			"type":    "sms",
			"content": "Database test notification",
		}

		resp, err := client.POST("/api/notification/notifications", notification)
		if err != nil {
			t.Errorf("Failed to create test notification: %v", err)
			return
		}

		if err := resp.AssertStatusCode(201); err != nil {
			t.Errorf("Notification creation failed: %v", err)
		}

		// Verify the data was persisted
		if err := dbUtils.AssertRecordExists(ctx, "notifications", map[string]interface{}{
			"user_id": "test-user-456",
			"type":    "sms",
		}); err != nil {
			t.Errorf("Notification not persisted to database: %v", err)
		}

		// Verify table counts
		if err := dbUtils.AssertTableCount(ctx, "notifications", 1); err != nil {
			t.Errorf("Unexpected notification count: %v", err)
		}

		t.Logf("Database snapshot created with %d tables", len(snapshot.Tables))
	})

	t.Run("Transaction Consistency", func(t *testing.T) {
		// Test that failed operations don't leave partial data
		gateway, _ := suite.GetService("apigateway")
		client := NewAPITestClient(gateway.BaseURL, suite.logger)

		// Attempt to create an invalid notification that should fail
		invalidNotification := map[string]interface{}{
			"user_id": "", // Invalid empty user_id
			"type":    "email",
			"content": "This should fail",
		}

		resp, err := client.POST("/api/notification/notifications", invalidNotification)
		if err == nil && resp.StatusCode < 400 {
			t.Errorf("Expected invalid notification to fail, but it succeeded")
		}

		// Verify no partial data was created
		count := 0
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE content = $1", 
			"This should fail").Scan(&count)
		if err != nil {
			t.Errorf("Failed to check for partial data: %v", err)
		}
		if count > 0 {
			t.Errorf("Found partial data from failed transaction: %d records", count)
		}
	})
}

// testEndToEndWorkflows tests complete user workflows
func testEndToEndWorkflows(t *testing.T, suite *IntegrationTestSuite) {
	gateway, err := suite.GetService("apigateway")
	if err != nil {
		t.Fatalf("Failed to get API Gateway service: %v", err)
	}

	client := NewAPITestClient(gateway.BaseURL, suite.logger)

	t.Run("User Onboarding Workflow", func(t *testing.T) {
		userID := "workflow-test-user-789"

		// Step 1: Create user profile
		profile := map[string]interface{}{
			"user_id":    userID,
			"first_name": "Workflow",
			"last_name":  "Test",
			"email":      "workflow@example.com",
		}

		resp, err := client.POST("/api/userprofile/profiles", profile)
		if err != nil {
			t.Errorf("Failed to create user profile: %v", err)
			return
		}
		if err := resp.AssertStatusCode(201); err != nil {
			t.Errorf("User profile creation failed: %v", err)
			return
		}

		// Step 2: Send welcome notification
		notification := map[string]interface{}{
			"user_id": userID,
			"type":    "email",
			"subject": "Welcome to ScholarPlatforms",
			"content": "Welcome to our platform!",
		}

		resp, err = client.POST("/api/notification/notifications", notification)
		if err != nil {
			t.Errorf("Failed to send welcome notification: %v", err)
			return
		}
		if err := resp.AssertStatusCode(201); err != nil {
			t.Errorf("Welcome notification failed: %v", err)
			return
		}

		// Step 3: Verify user profile exists
		resp, err = client.GET(fmt.Sprintf("/api/userprofile/profiles/%s", userID))
		if err != nil {
			t.Errorf("Failed to retrieve user profile: %v", err)
			return
		}
		if err := resp.AssertStatusCode(200); err != nil {
			t.Errorf("User profile retrieval failed: %v", err)
			return
		}
		if err := resp.AssertJSONField("email", "workflow@example.com"); err != nil {
			t.Errorf("User profile data incorrect: %v", err)
		}

		// Step 4: Verify notification was sent
		resp, err = client.GET(fmt.Sprintf("/api/notification/notifications?user_id=%s", userID))
		if err != nil {
			t.Errorf("Failed to retrieve notifications: %v", err)
			return
		}
		if err := resp.AssertStatusCode(200); err != nil {
			t.Errorf("Notification retrieval failed: %v", err)
		}
	})
}

// testPerformance runs performance and load tests
func testPerformance(t *testing.T, suite *IntegrationTestSuite) {
	gateway, err := suite.GetService("apigateway")
	if err != nil {
		t.Fatalf("Failed to get API Gateway service: %v", err)
	}

	client := NewAPITestClient(gateway.BaseURL, suite.logger)
	ctx := context.Background()

	t.Run("Health Check Load Test", func(t *testing.T) {
		scenario := LoadTestScenario{
			Name:       "health_check_load",
			Concurrent: 10,
			Duration:   30 * time.Second,
			Requests: []APITestRequest{
				{Method: "GET", Endpoint: "/health"},
			},
			Assertions: []APITestAssertion{
				{Field: "status_code", Expected: 200, Description: "Health check should return 200"},
				{Field: "duration", Expected: "500ms", Description: "Response should be under 500ms"},
			},
		}

		result, err := client.RunLoadTest(ctx, scenario)
		if err != nil {
			t.Errorf("Load test failed: %v", err)
			return
		}

		// Assert performance metrics
		if result.SuccessfulRequests < result.TotalRequests*90/100 {
			t.Errorf("Success rate too low: %d/%d (%.2f%%)",
				result.SuccessfulRequests, result.TotalRequests,
				float64(result.SuccessfulRequests)/float64(result.TotalRequests)*100)
		}

		if result.AverageLatency > 500*time.Millisecond {
			t.Errorf("Average latency too high: %v", result.AverageLatency)
		}

		if result.RequestsPerSecond < 50 {
			t.Errorf("Throughput too low: %.2f RPS", result.RequestsPerSecond)
		}

		t.Logf("Load test results: %d requests, %.2f RPS, %v avg latency, %d errors",
			result.TotalRequests, result.RequestsPerSecond, result.AverageLatency, len(result.Errors))
	})

	t.Run("API Gateway Routing Load Test", func(t *testing.T) {
		scenario := LoadTestScenario{
			Name:       "routing_load",
			Concurrent: 5,
			Duration:   60 * time.Second,
			Requests: []APITestRequest{
				{Method: "GET", Endpoint: "/api/notification/health"},
				{Method: "GET", Endpoint: "/api/userprofile/health"},
			},
		}

		result, err := client.RunLoadTest(ctx, scenario)
		if err != nil {
			t.Errorf("Routing load test failed: %v", err)
			return
		}

		if result.FailedRequests > result.TotalRequests*5/100 {
			t.Errorf("Too many failed requests: %d/%d", result.FailedRequests, result.TotalRequests)
		}

		t.Logf("Routing load test: %d requests, %.2f%% success rate",
			result.TotalRequests,
			float64(result.SuccessfulRequests)/float64(result.TotalRequests)*100)
	})
}

// Helper method for the test suite
func (suite *IntegrationTestSuite) setupTestEnvironment(ctx context.Context, config *TestConfig) error {
	// This would load the test configuration and set up the test environment
	// For now, we'll use a default configuration
	*config = TestConfig{
		DatabaseImage:   "cockroachdb/cockroach:latest",
		DatabasePort:    26257,
		RedisImage:      "redis:7-alpine",
		StartupTimeout:  60 * time.Second,
		RequestTimeout:  30 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		NetworkName:     "scholarplatforms-test-network",
	}
	
	return suite.SetupInfrastructure(*config)
}