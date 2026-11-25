package eventsourcing

import (
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// NATSEventBus implements EventBus using NATS JetStream
type NATSEventBus struct {
	conn        *nats.Conn
	js          nats.JetStreamContext
	logger      logger.Logger
	streamName  string
	subscribers map[string]*nats.Subscription
	mu          sync.RWMutex
	ownsConn    bool // true if this bus created the connection and should close it
}

// NATSConfig holds NATS configuration
type NATSConfig struct {
	URL        string
	StreamName string
	Subjects   []string   // Subjects to create in the stream
	Conn       *nats.Conn // Optional: reuse an existing NATS connection
}

// NewNATSEventBus creates a new NATS event bus.
// If cfg.Conn is provided, it reuses that connection instead of creating a new one.
// This avoids duplicate connections with mismatched reconnect settings.
func NewNATSEventBus(cfg NATSConfig, log logger.Logger) (*NATSEventBus, error) {
	var conn *nats.Conn
	var ownsConn bool

	if cfg.Conn != nil {
		// Reuse the provided connection
		conn = cfg.Conn
		ownsConn = false
	} else {
		// Create a new connection with proper reconnect handling
		var err error
		conn, err = nats.Connect(cfg.URL,
			nats.MaxReconnects(-1), // Unlimited reconnects
			nats.ReconnectWait(2*time.Second),
			nats.ReconnectBufSize(16*1024*1024), // 16MB buffer during reconnect
			nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
				if err != nil {
					log.Warn("NATS EventBus disconnected", logger.ErrorField(err))
				}
			}),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				log.Info("NATS EventBus reconnected", logger.String("url", nc.ConnectedUrl()))
			}),
			nats.ClosedHandler(func(nc *nats.Conn) {
				log.Error("NATS EventBus connection closed permanently")
			}),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to NATS: %w", err)
		}
		ownsConn = true
	}

	// Create JetStream context
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	bus := &NATSEventBus{
		conn:        conn,
		js:          js,
		logger:      log,
		streamName:  cfg.StreamName,
		subscribers: make(map[string]*nats.Subscription),
		ownsConn:    ownsConn,
	}

	// Create or update stream
	if err := bus.ensureStream(cfg.Subjects); err != nil {
		return nil, fmt.Errorf("failed to ensure stream: %w", err)
	}

	log.Info("NATS Event Bus initialized",
		logger.String("stream", cfg.StreamName),
		logger.String("url", conn.ConnectedUrl()),
	)

	return bus, nil
}

// ensureStream creates or updates the JetStream stream
func (nb *NATSEventBus) ensureStream(subjects []string) error {
	// Default subjects if none provided
	if len(subjects) == 0 {
		subjects = []string{
			"events.>", // Catch all events
		}
	}

	streamConfig := &nats.StreamConfig{
		Name:     nb.streamName,
		Subjects: subjects,
		// Domain events are fan-out facts: activity logs, notifications, analytics,
		// and Intelligence must each be able to maintain an independent durable.
		Retention:  nats.LimitsPolicy,
		MaxAge:     90 * 24 * time.Hour, // 90 days retention
		Storage:    nats.FileStorage,
		Replicas:   1, // Use 1 for single-node; increase for HA clusters
		Duplicates: 2 * time.Minute,
	}

	// Try to get existing stream info
	streamInfo, err := nb.js.StreamInfo(nb.streamName)
	if err != nil {
		// Stream doesn't exist, create it
		if err == nats.ErrStreamNotFound {
			_, err = nb.js.AddStream(streamConfig)
			if err != nil {
				return fmt.Errorf("failed to create stream: %w", err)
			}
			nb.logger.Info("Created NATS stream",
				logger.String("stream", nb.streamName),
			)
			return nil
		}
		return fmt.Errorf("failed to get stream info: %w", err)
	}

	// Stream exists, update if necessary
	if streamInfo != nil {
		_, err = nb.js.UpdateStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
		nb.logger.Info("Updated NATS stream",
			logger.String("stream", nb.streamName),
		)
	}

	return nil
}

// Publish publishes an event to NATS
func (nb *NATSEventBus) Publish(event *Event) error {
	return nb.PublishBatch(event)
}

// PublishBatch publishes multiple events to NATS
func (nb *NATSEventBus) PublishBatch(events ...*Event) error {
	for _, event := range events {
		// Convert event to JSON
		data, err := event.ToJSON()
		if err != nil {
			nb.logger.Error("Failed to marshal event",
				logger.ErrorField(err),
				logger.String("event_type", event.EventType),
			)
			return fmt.Errorf("failed to marshal event: %w", err)
		}

		// Construct subject from event type
		// e.g., "events.user.created" or "events.subject.updated"
		subject := fmt.Sprintf("events.%s", event.EventType)

		// Publish to JetStream
		_, err = nb.js.Publish(subject, data)
		if err != nil {
			nb.logger.Error("Failed to publish event",
				logger.ErrorField(err),
				logger.String("subject", subject),
				logger.String("event_type", event.EventType),
			)
			return fmt.Errorf("failed to publish event: %w", err)
		}

		nb.logger.Debug("Event published",
			logger.String("event_id", event.ID),
			logger.String("event_type", event.EventType),
			logger.String("subject", subject),
		)
	}

	return nil
}

// Subscribe subscribes to a specific event type
func (nb *NATSEventBus) Subscribe(eventType string, handler EventHandler) error {
	// Construct subject from event type
	subject := fmt.Sprintf("events.%s", eventType)

	// Create durable consumer name from event type
	consumerName := fmt.Sprintf("consumer-%s", eventType)

	// Subscribe to the subject
	sub, err := nb.js.Subscribe(subject, func(msg *nats.Msg) {
		// Parse event
		event, err := FromJSON(msg.Data)
		if err != nil {
			nb.logger.Error("Failed to parse event",
				logger.ErrorField(err),
				logger.String("subject", subject),
			)
			msg.Nak() // Negative acknowledgment
			return
		}

		// Handle event
		if err := handler(event); err != nil {
			nb.logger.Error("Event handler failed",
				logger.ErrorField(err),
				logger.String("event_id", event.ID),
				logger.String("event_type", event.EventType),
			)
			msg.Nak() // Negative acknowledgment - will be redelivered
			return
		}

		// Acknowledge successful processing
		msg.Ack()

		nb.logger.Debug("Event processed",
			logger.String("event_id", event.ID),
			logger.String("event_type", event.EventType),
		)
	}, nats.Durable(consumerName), nats.ManualAck())

	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}

	// Store subscription
	nb.mu.Lock()
	nb.subscribers[subject] = sub
	nb.mu.Unlock()

	nb.logger.Info("Subscribed to events",
		logger.String("event_type", eventType),
		logger.String("subject", subject),
	)

	return nil
}

// SubscribeAll subscribes to all events
func (nb *NATSEventBus) SubscribeAll(handler EventHandler) error {
	subject := "events.>"
	consumerName := "consumer-all-events"

	// Subscribe to all events
	sub, err := nb.js.Subscribe(subject, func(msg *nats.Msg) {
		// Parse event
		event, err := FromJSON(msg.Data)
		if err != nil {
			nb.logger.Error("Failed to parse event",
				logger.ErrorField(err),
			)
			msg.Nak()
			return
		}

		// Handle event
		if err := handler(event); err != nil {
			nb.logger.Error("Event handler failed",
				logger.ErrorField(err),
				logger.String("event_id", event.ID),
				logger.String("event_type", event.EventType),
			)
			msg.Nak()
			return
		}

		msg.Ack()
	}, nats.Durable(consumerName), nats.ManualAck())

	if err != nil {
		return fmt.Errorf("failed to subscribe to all events: %w", err)
	}

	// Store subscription
	nb.mu.Lock()
	nb.subscribers[subject] = sub
	nb.mu.Unlock()

	nb.logger.Info("Subscribed to all events")

	return nil
}

// Close closes the NATS connection (only if this bus owns it)
func (nb *NATSEventBus) Close() error {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	// Unsubscribe all
	for subject, sub := range nb.subscribers {
		if err := sub.Unsubscribe(); err != nil {
			nb.logger.Error("Failed to unsubscribe",
				logger.ErrorField(err),
				logger.String("subject", subject),
			)
		}
	}

	// Only close the NATS connection if we created it
	if nb.ownsConn {
		nb.conn.Close()
	}

	nb.logger.Info("NATS Event Bus closed")

	return nil
}

// GetStreamInfo returns information about the stream
func (nb *NATSEventBus) GetStreamInfo() (*nats.StreamInfo, error) {
	return nb.js.StreamInfo(nb.streamName)
}

// PurgeStream purges all messages from the stream (use with caution!)
func (nb *NATSEventBus) PurgeStream() error {
	err := nb.js.PurgeStream(nb.streamName)
	if err != nil {
		return fmt.Errorf("failed to purge stream: %w", err)
	}

	nb.logger.Warn("Stream purged",
		logger.String("stream", nb.streamName),
	)

	return nil
}

// GetConsumers returns list of consumers for the stream
func (nb *NATSEventBus) GetConsumers() ([]*nats.ConsumerInfo, error) {
	var consumers []*nats.ConsumerInfo

	for info := range nb.js.Consumers(nb.streamName) {
		consumers = append(consumers, info)
	}

	return consumers, nil
}
