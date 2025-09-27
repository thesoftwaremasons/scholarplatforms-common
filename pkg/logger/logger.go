package logger

import (
	"context"
	"time"
)

// Field represents a structured log field
type Field struct {
	Key   string
	Value any
}

// LoggerKey is used as a context key for logger instances
type LoggerKey struct{}

// Logger defines the interface for structured logging
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
}

// LogHook defines a function that can intercept log entries
type LogHook func(level string, msg string, fields []Field, timestamp time.Time)

// Hookable defines a logger that supports hooks for external systems
type Hookable interface {
	AddHook(hook LogHook)
	RemoveHook(hook LogHook)
}

// Closeable defines a logger that can be closed to free resources
type Closeable interface {
	Close() error
}

// Convenience functions for creating common field types
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

func Time(key string, value time.Time) Field {
	return Field{Key: key, Value: value}
}

func ErrorField(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

func Any(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// FromContext extracts a logger from the context, returns nil if not found
func FromContext(ctx context.Context) Logger {
	if logger, ok := ctx.Value(LoggerKey{}).(Logger); ok {
		return logger
	}
	return nil
}

// ToContext adds a logger to the context
func ToContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, LoggerKey{}, logger)
}