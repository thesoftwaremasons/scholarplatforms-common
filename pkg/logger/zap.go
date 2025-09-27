package logger

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger implements the Logger interface using zap
type ZapLogger struct {
	logger *zap.Logger
	hooks  []LogHook
	mu     sync.RWMutex
}

// ZapConfig holds configuration for the zap logger
type ZapConfig struct {
	Level      string `yaml:"level"`       // debug, info, warn, error
	Format     string `yaml:"format"`      // json, console
	Output     string `yaml:"output"`      // stdout, stderr, or file path
	TimeFormat string `yaml:"time_format"` // time format for logs
}

// NewZapLogger creates a new zap-based logger with the given configuration
func NewZapLogger(config ZapConfig) (*ZapLogger, error) {
	// Set defaults
	if config.Level == "" {
		config.Level = "info"
	}
	if config.Format == "" {
		config.Format = "json"
	}
	if config.Output == "" {
		config.Output = "stdout"
	}
	if config.TimeFormat == "" {
		config.TimeFormat = time.RFC3339
	}

	// Parse log level
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		return nil, err
	}

	// Configure encoder
	var encoderConfig zapcore.EncoderConfig
	if config.Format == "console" {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encoderConfig = zap.NewProductionEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	}
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(config.TimeFormat)

	// Create encoder
	var encoder zapcore.Encoder
	if config.Format == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Configure output
	var writeSyncer zapcore.WriteSyncer
	switch config.Output {
	case "stdout":
		writeSyncer = zapcore.Lock(zapcore.AddSync(zapcore.NewMultiWriteSyncer()))
	case "stderr":
		writeSyncer = zapcore.Lock(zapcore.AddSync(zapcore.NewMultiWriteSyncer()))
	default:
		// File output would be implemented here
		writeSyncer = zapcore.Lock(zapcore.AddSync(zapcore.NewMultiWriteSyncer()))
	}

	// Create core and logger
	core := zapcore.NewCore(encoder, writeSyncer, level)
	zapLogger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &ZapLogger{
		logger: zapLogger,
		hooks:  make([]LogHook, 0),
	}, nil
}

// NewDevelopmentLogger creates a pre-configured logger for development
func NewDevelopmentLogger() (*ZapLogger, error) {
	return NewZapLogger(ZapConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
}

// NewProductionLogger creates a pre-configured logger for production
func NewProductionLogger() (*ZapLogger, error) {
	return NewZapLogger(ZapConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
}

// Debug logs a debug message
func (z *ZapLogger) Debug(msg string, fields ...Field) {
	zapFields := z.convertFields(fields)
	z.logger.Debug(msg, zapFields...)
	z.callHooks("debug", msg, fields)
}

// Info logs an info message
func (z *ZapLogger) Info(msg string, fields ...Field) {
	zapFields := z.convertFields(fields)
	z.logger.Info(msg, zapFields...)
	z.callHooks("info", msg, fields)
}

// Warn logs a warning message
func (z *ZapLogger) Warn(msg string, fields ...Field) {
	zapFields := z.convertFields(fields)
	z.logger.Warn(msg, zapFields...)
	z.callHooks("warn", msg, fields)
}

// Error logs an error message
func (z *ZapLogger) Error(msg string, fields ...Field) {
	zapFields := z.convertFields(fields)
	z.logger.Error(msg, zapFields...)
	z.callHooks("error", msg, fields)
}

// Fatal logs a fatal message and exits
func (z *ZapLogger) Fatal(msg string, fields ...Field) {
	zapFields := z.convertFields(fields)
	z.logger.Fatal(msg, zapFields...)
	z.callHooks("fatal", msg, fields)
}

// With returns a new logger with additional fields
func (z *ZapLogger) With(fields ...Field) Logger {
	zapFields := z.convertFields(fields)
	newLogger := z.logger.With(zapFields...)
	return &ZapLogger{
		logger: newLogger,
		hooks:  z.hooks, // Share hooks
	}
}

// WithContext returns a new logger with context
func (z *ZapLogger) WithContext(ctx context.Context) Logger {
	// Extract any context fields if needed
	return z
}

// AddHook adds a log hook
func (z *ZapLogger) AddHook(hook LogHook) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.hooks = append(z.hooks, hook)
}

// RemoveHook removes a log hook
func (z *ZapLogger) RemoveHook(hook LogHook) {
	z.mu.Lock()
	defer z.mu.Unlock()
	for i, h := range z.hooks {
		// Compare function pointers (this is a simplified comparison)
		if &h == &hook {
			z.hooks = append(z.hooks[:i], z.hooks[i+1:]...)
			break
		}
	}
}

// Close closes the logger
func (z *ZapLogger) Close() error {
	return z.logger.Sync()
}

// convertFields converts our Field type to zap.Field
func (z *ZapLogger) convertFields(fields []Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key, field.Value)
	}
	return zapFields
}

// callHooks calls all registered hooks
func (z *ZapLogger) callHooks(level, msg string, fields []Field) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	
	timestamp := time.Now()
	for _, hook := range z.hooks {
		hook(level, msg, fields, timestamp)
	}
}