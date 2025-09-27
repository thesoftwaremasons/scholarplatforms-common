# Scholar Platforms Common

A shared library containing common functionality used across all Scholar Platforms microservices.

## 🚀 Features

### 📊 **Observability**
- **Structured Logging** - Zap-based logger with multiple output formats
- **Distributed Tracing** - OpenTelemetry integration with Jaeger
- **Metrics Collection** - Prometheus metrics with HTTP middleware

### 🔐 **Security**
- **JWT Authentication** - Token generation, validation, and management
- **CORS Middleware** - Configurable Cross-Origin Resource Sharing
- **Rate Limiting** - Token bucket rate limiting with IP-based tracking

### 🛠 **Middleware**
- **Authentication** - JWT-based auth middleware with role checking
- **Logging** - HTTP request/response logging with panic recovery
- **CORS** - Cross-origin request handling
- **Rate Limiting** - Request throttling and abuse prevention

### ⚙️ **Configuration**
- **Unified Config** - Standardized configuration structures
- **Environment Support** - Development, staging, production configs
- **YAML Support** - Human-readable configuration files

### 🔧 **Utilities**
- **HTTP Client** - Configured HTTP client with logging and metrics
- **Validation** - Email, password, UUID, and URL validation
- **String Utils** - Sanitization and manipulation functions

## 📦 Installation

```bash
go get github.com/thesoftwaremasons/scholarplatforms-common
```

## 🏗 Usage Examples

### Logger Setup

```go
import "github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"

// Development logger
log, err := logger.NewDevelopmentLogger()
if err != nil {
    panic(err)
}

// Production logger
log, err := logger.NewProductionLogger()
if err != nil {
    panic(err)
}

// Usage
log.Info("Service started", 
    logger.String("service", "user-profile"),
    logger.Int("port", 8080),
)
```

### Tracing Setup

```go
import "github.com/thesoftwaremasons/scholarplatforms-common/pkg/tracing"

config := config.TracingConfig{
    Enabled:        true,
    ServiceName:    "user-profile-service",
    JaegerEndpoint: "http://jaeger:14268/api/traces",
    SampleRate:     1.0,
}

tracingManager, err := tracing.NewTracingManager(config)
if err != nil {
    panic(err)
}
defer tracingManager.Shutdown(context.Background())

// Start a span
ctx, span := tracingManager.StartSpan(context.Background(), "operation-name")
defer span.End()
```

### JWT Authentication

```go
import "github.com/thesoftwaremasons/scholarplatforms-common/pkg/auth"

jwtManager := auth.NewJWTManager(
    "your-secret-key",
    24*time.Hour,   // token expiry
    7*24*time.Hour, // refresh token expiry
)

// Generate token
token, err := jwtManager.GenerateToken("user-123", "user@example.com", "user", "org-456")
if err != nil {
    panic(err)
}

// Validate token
claims, err := jwtManager.ValidateToken(token)
if err != nil {
    panic(err)
}
```

### Middleware Usage

```go
import (
    "github.com/thesoftwaremasons/scholarplatforms-common/pkg/middleware"
    "github.com/thesoftwaremasons/scholarplatforms-common/pkg/config"
)

// Setup middleware
authMiddleware := middleware.NewAuthMiddleware(jwtManager, logger)
corsMiddleware := middleware.NewCORSMiddleware(config.CORSConfig{
    AllowedOrigins: []string{"https://app.scholarplatforms.com"},
    AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
})
rateLimitMiddleware := middleware.NewRateLimitMiddleware(config.RateLimitConfig{
    Enabled:           true,
    RequestsPerMinute: 60,
    Burst:             10,
}, logger)

// Apply to HTTP server
mux := http.NewServeMux()
handler := corsMiddleware.Handler()(
    rateLimitMiddleware.Handler()(
        authMiddleware.RequireAuth()(mux)))
```

### Metrics Collection

```go
import "github.com/thesoftwaremasons/scholarplatforms-common/pkg/metrics"

metricsManager := metrics.NewMetricsManager("user-profile-service")

// Record HTTP request
metricsManager.RecordHTTPRequest("GET", "/users", 200, time.Millisecond*150)

// Track connections
metricsManager.IncActiveConnections()
defer metricsManager.DecActiveConnections()

// Expose metrics endpoint
http.Handle("/metrics", metricsManager.Handler())
```

### HTTP Client

```go
import "github.com/thesoftwaremasons/scholarplatforms-common/pkg/utils"

client := utils.NewHTTPClient(30*time.Second, logger)

// GET request
resp, err := client.Get(ctx, "https://api.example.com/users", map[string]string{
    "Authorization": "Bearer " + token,
})

// POST JSON
data := map[string]interface{}{"name": "John Doe"}
resp, err := client.PostJSON(ctx, "https://api.example.com/users", data, nil)
```

## 📁 Package Structure

```
pkg/
├── auth/           # JWT token management
├── config/         # Configuration structures
├── logger/         # Structured logging
├── metrics/        # Prometheus metrics
├── middleware/     # HTTP middleware
├── tracing/        # OpenTelemetry tracing
└── utils/          # Utility functions
```

## 🔧 Configuration

The common package uses standardized configuration structures:

```yaml
# Service configuration
server:
  host: "0.0.0.0"
  port: 8080
  environment: "production"

# Database configuration
database:
  driver: "postgres"
  host: "localhost"
  port: 5432
  database: "scholarplatforms"

# Logging configuration
logger:
  level: "info"
  format: "json"
  output: "stdout"

# Tracing configuration
tracing:
  enabled: true
  service_name: "my-service"
  jaeger_endpoint: "http://jaeger:14268/api/traces"
  sample_rate: 1.0

# Authentication configuration
auth:
  jwt_secret: "your-secret-key"
  token_expiry: "24h"
  refresh_token_expiry: "168h"

# CORS configuration
cors:
  allowed_origins: ["*"]
  allowed_methods: ["GET", "POST", "PUT", "DELETE"]
  allowed_headers: ["Content-Type", "Authorization"]

# Rate limiting configuration
rate_limit:
  enabled: true
  requests_per_minute: 60
  burst: 10
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🔗 Related Services

- [Scholar Platforms API Gateway](../scholarplatforms-apigateway)
- [Scholar Platforms User Profile](../scholarplatforms-userprofile)
- [Scholar Platforms Notification](../scholarplatforms-notification)
- [Scholar Platforms File Storage](../scholarplatforms-filestorage)
- [Scholar Platforms Auth](../scholarplatforms-auth)