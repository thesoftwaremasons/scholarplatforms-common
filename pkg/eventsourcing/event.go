package eventsourcing

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event represents a domain event
type Event struct {
	ID            string                 `json:"id"`
	AggregateID   string                 `json:"aggregate_id"`
	AggregateType string                 `json:"aggregate_type"`
	EventType     string                 `json:"event_type"`
	EventVersion  string                 `json:"event_version"`
	Data          map[string]interface{} `json:"data"`
	Metadata      map[string]interface{} `json:"metadata"`
	Timestamp     time.Time              `json:"timestamp"`
	UserID        string                 `json:"user_id,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	CausationID   string                 `json:"causation_id,omitempty"`
}

// NewEvent creates a new event
func NewEvent(aggregateID, aggregateType, eventType string, data map[string]interface{}) *Event {
	return &Event{
		ID:            uuid.New().String(),
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
		EventType:     eventType,
		EventVersion:  "1.0",
		Data:          data,
		Metadata:      make(map[string]interface{}),
		Timestamp:     time.Now().UTC(),
	}
}

// WithUserID sets the user ID who triggered the event
func (e *Event) WithUserID(userID string) *Event {
	e.UserID = userID
	return e
}

// WithCorrelationID sets the correlation ID
func (e *Event) WithCorrelationID(correlationID string) *Event {
	e.CorrelationID = correlationID
	return e
}

// WithCausationID sets the causation ID (the event that caused this event)
func (e *Event) WithCausationID(causationID string) *Event {
	e.CausationID = causationID
	return e
}

// WithMetadata adds metadata to the event
func (e *Event) WithMetadata(key string, value interface{}) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// ToJSON converts the event to JSON
func (e *Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON creates an event from JSON
func FromJSON(data []byte) (*Event, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// EventStore interface defines the contract for event storage
type EventStore interface {
	// Append appends events to the store
	Append(events ...*Event) error

	// Load loads events for an aggregate
	Load(aggregateID string) ([]*Event, error)

	// LoadByType loads events by type
	LoadByType(eventType string, since time.Time) ([]*Event, error)

	// LoadAll loads all events since a given time
	LoadAll(since time.Time, limit int) ([]*Event, error)
}

// EventBus interface defines the contract for event publishing
type EventBus interface {
	// Publish publishes an event
	Publish(event *Event) error

	// PublishBatch publishes multiple events
	PublishBatch(events ...*Event) error

	// Subscribe subscribes to events
	Subscribe(eventType string, handler EventHandler) error

	// SubscribeAll subscribes to all events
	SubscribeAll(handler EventHandler) error

	// Close closes the event bus
	Close() error
}

// EventHandler is a function that handles events
type EventHandler func(event *Event) error

// EventProcessor combines EventStore and EventBus
type EventProcessor struct {
	store EventStore
	bus   EventBus
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(store EventStore, bus EventBus) *EventProcessor {
	return &EventProcessor{
		store: store,
		bus:   bus,
	}
}

// Process processes an event (stores and publishes)
func (ep *EventProcessor) Process(events ...*Event) error {
	// Store events
	if err := ep.store.Append(events...); err != nil {
		return err
	}

	// Publish events
	if err := ep.bus.PublishBatch(events...); err != nil {
		return err
	}

	return nil
}

// GetStore returns the event store
func (ep *EventProcessor) GetStore() EventStore {
	return ep.store
}

// GetBus returns the event bus
func (ep *EventProcessor) GetBus() EventBus {
	return ep.bus
}

// Common event types for the platform
const (
	// User events
	EventTypeUserCreated      = "user.created"
	EventTypeUserUpdated      = "user.updated"
	EventTypeUserDeleted      = "user.deleted"
	EventTypeUserLoggedIn     = "user.logged_in"
	EventTypeUserLoggedOut    = "user.logged_out"
	EventTypeUserPasswordChanged = "user.password_changed"

	// School events
	EventTypeSchoolCreated    = "school.created"
	EventTypeSchoolUpdated    = "school.updated"
	EventTypeSchoolDeleted    = "school.deleted"
	EventTypeSchoolActivated  = "school.activated"
	EventTypeSchoolDeactivated = "school.deactivated"

	// Subject events (Academic module)
	EventTypeSubjectCreated   = "subject.created"
	EventTypeSubjectUpdated   = "subject.updated"
	EventTypeSubjectDeleted   = "subject.deleted"
	EventTypeSubjectRenamed   = "subject.renamed"
	EventTypeSubjectArchived  = "subject.archived"

	// Notification events
	EventTypeNotificationSent       = "notification.sent"
	EventTypeNotificationDelivered  = "notification.delivered"
	EventTypeNotificationFailed     = "notification.failed"
	EventTypeNotificationRead       = "notification.read"

	// File events
	EventTypeFileUploaded   = "file.uploaded"
	EventTypeFileDeleted    = "file.deleted"
	EventTypeFileShared     = "file.shared"
	EventTypeFileDownloaded = "file.downloaded"

	// Assessment events
	EventTypeAssessmentCreated   = "assessment.created"
	EventTypeAssessmentSubmitted = "assessment.submitted"
	EventTypeAssessmentGraded    = "assessment.graded"
)

// Aggregate types
const (
	AggregateTypeUser         = "User"
	AggregateTypeSchool       = "School"
	AggregateTypeSubject      = "Subject"
	AggregateTypeNotification = "Notification"
	AggregateTypeFile         = "File"
	AggregateTypeAssessment   = "Assessment"
)
