package eventsourcing

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// ActivityCategory represents the category of an activity
type ActivityCategory string

const (
	CategoryAcademics  ActivityCategory = "academics"
	CategoryStudents   ActivityCategory = "students"
	CategoryBehavior   ActivityCategory = "behavior"
	CategoryAttendance ActivityCategory = "attendance"
	CategoryForms      ActivityCategory = "forms"
	CategoryFinance    ActivityCategory = "finance"
	CategoryStaff      ActivityCategory = "staff"
	CategorySettings   ActivityCategory = "settings"
	CategorySecurity   ActivityCategory = "security"
	CategorySystem     ActivityCategory = "system"
)

// ActivitySeverity represents the severity level of an activity
type ActivitySeverity string

const (
	SeverityInfo     ActivitySeverity = "info"
	SeveritySuccess  ActivitySeverity = "success"
	SeverityWarning  ActivitySeverity = "warning"
	SeverityError    ActivitySeverity = "error"
	SeverityCritical ActivitySeverity = "critical"
)

// ActivityAction represents the action performed
type ActivityAction string

const (
	ActionCreated    ActivityAction = "created"
	ActionUpdated    ActivityAction = "updated"
	ActionDeleted    ActivityAction = "deleted"
	ActionArchived   ActivityAction = "archived"
	ActionRestored   ActivityAction = "restored"
	ActionAssigned   ActivityAction = "assigned"
	ActionUnassigned ActivityAction = "unassigned"
	ActionEnrolled   ActivityAction = "enrolled"
	ActionUnenrolled ActivityAction = "unenrolled"
	ActionPublished  ActivityAction = "published"
	ActionSubmitted  ActivityAction = "submitted"
	ActionApproved   ActivityAction = "approved"
	ActionRejected   ActivityAction = "rejected"
	ActionGraded     ActivityAction = "graded"
	ActionViewed     ActivityAction = "viewed"
	ActionExported   ActivityAction = "exported"
	ActionImported   ActivityAction = "imported"
	ActionConfigured ActivityAction = "configured"
)

// ActivityEvent extends Event with activity-specific fields
type ActivityEvent struct {
	*Event

	// Activity-specific fields (stored in Metadata)
	SchoolID      string           `json:"school_id"`
	Category      ActivityCategory `json:"category"`
	Subcategory   string           `json:"subcategory,omitempty"`
	Severity      ActivitySeverity `json:"severity"`
	ActorName     string           `json:"actor_name"`
	ActorRole     string           `json:"actor_role,omitempty"`
	ActorType     string           `json:"actor_type"` // user, system, integration
	EntityName    string           `json:"entity_name"`
	Action        ActivityAction   `json:"action"`
	Narrative     string           `json:"narrative"`
	NarrativeHTML string           `json:"narrative_html,omitempty"`
	DeepLink      string           `json:"deep_link,omitempty"`
	Source        string           `json:"source"` // web, api, mobile, system
	IPAddress     string           `json:"ip_address,omitempty"`
	UserAgent     string           `json:"user_agent,omitempty"`
	Changes       map[string]any   `json:"changes,omitempty"`
	PreviousState map[string]any   `json:"previous_state,omitempty"`
	CurrentState  map[string]any   `json:"current_state,omitempty"`
}

// ActivityPublisher provides a convenient way to publish activity events
type ActivityPublisher struct {
	bus         EventBus
	logger      logger.Logger
	serviceName string
}

// NewActivityPublisher creates a new activity publisher
func NewActivityPublisher(bus EventBus, serviceName string, log logger.Logger) *ActivityPublisher {
	return &ActivityPublisher{
		bus:         bus,
		logger:      log,
		serviceName: serviceName,
	}
}

// ActivityBuilder helps build activity events fluently
type ActivityBuilder struct {
	publisher     *ActivityPublisher
	schoolID      string
	category      ActivityCategory
	subcategory   string
	severity      ActivitySeverity
	actorID       string
	actorName     string
	actorRole     string
	actorType     string
	entityType    string
	entityID      string
	entityName    string
	action        ActivityAction
	eventType     string
	narrative     string
	narrativeHTML string
	deepLink      string
	source        string
	ipAddress     string
	userAgent     string
	changes       map[string]any
	previousState map[string]any
	currentState  map[string]any
	correlationID string
	requestID     string
}

// NewActivity starts building an activity event
func (p *ActivityPublisher) NewActivity(schoolID string) *ActivityBuilder {
	return &ActivityBuilder{
		publisher: p,
		schoolID:  schoolID,
		severity:  SeverityInfo,
		actorType: "user",
		source:    "web",
	}
}

// Category sets the activity category
func (b *ActivityBuilder) Category(cat ActivityCategory) *ActivityBuilder {
	b.category = cat
	return b
}

// Subcategory sets the activity subcategory
func (b *ActivityBuilder) Subcategory(sub string) *ActivityBuilder {
	b.subcategory = sub
	return b
}

// Severity sets the activity severity
func (b *ActivityBuilder) Severity(sev ActivitySeverity) *ActivityBuilder {
	b.severity = sev
	return b
}

// Actor sets who performed the action
func (b *ActivityBuilder) Actor(userID, name, role string) *ActivityBuilder {
	b.actorID = userID
	b.actorName = name
	b.actorRole = role
	return b
}

// ActorType sets the type of actor (user, system, integration)
func (b *ActivityBuilder) ActorType(actorType string) *ActivityBuilder {
	b.actorType = actorType
	return b
}

// Entity sets what was affected
func (b *ActivityBuilder) Entity(entityType, entityID, entityName string) *ActivityBuilder {
	b.entityType = entityType
	b.entityID = entityID
	b.entityName = entityName
	return b
}

// Action sets what happened
func (b *ActivityBuilder) Action(action ActivityAction) *ActivityBuilder {
	b.action = action
	return b
}

// EventType sets the specific event type (e.g., "subject.created")
func (b *ActivityBuilder) EventType(eventType string) *ActivityBuilder {
	b.eventType = eventType
	return b
}

// Narrative sets the human-readable description
func (b *ActivityBuilder) Narrative(narrative string) *ActivityBuilder {
	b.narrative = narrative
	return b
}

// NarrativeHTML sets the HTML-formatted description
func (b *ActivityBuilder) NarrativeHTML(html string) *ActivityBuilder {
	b.narrativeHTML = html
	return b
}

// DeepLink sets the navigation link
func (b *ActivityBuilder) DeepLink(link string) *ActivityBuilder {
	b.deepLink = link
	return b
}

// Source sets the source of the activity (web, api, mobile, system)
func (b *ActivityBuilder) Source(source string) *ActivityBuilder {
	b.source = source
	return b
}

// IPAddress sets the IP address
func (b *ActivityBuilder) IPAddress(ip string) *ActivityBuilder {
	b.ipAddress = ip
	return b
}

// UserAgent sets the user agent
func (b *ActivityBuilder) UserAgent(ua string) *ActivityBuilder {
	b.userAgent = ua
	return b
}

// Changes sets the field changes
func (b *ActivityBuilder) Changes(changes map[string]any) *ActivityBuilder {
	b.changes = changes
	return b
}

// PreviousState sets the previous state
func (b *ActivityBuilder) PreviousState(state map[string]any) *ActivityBuilder {
	b.previousState = state
	return b
}

// CurrentState sets the current state
func (b *ActivityBuilder) CurrentState(state map[string]any) *ActivityBuilder {
	b.currentState = state
	return b
}

// CorrelationID sets the correlation ID for tracing
func (b *ActivityBuilder) CorrelationID(id string) *ActivityBuilder {
	b.correlationID = id
	return b
}

// RequestID sets the request ID
func (b *ActivityBuilder) RequestID(id string) *ActivityBuilder {
	b.requestID = id
	return b
}

// generateNarrative generates a human-readable narrative if not provided
func (b *ActivityBuilder) generateNarrative() string {
	if b.narrative != "" {
		return b.narrative
	}

	// Generate default narrative
	actorDisplay := b.actorName
	if actorDisplay == "" {
		actorDisplay = "Someone"
	}

	entityDisplay := b.entityName
	if entityDisplay == "" {
		entityDisplay = b.entityType
	}

	action := string(b.action)
	if action == "" {
		action = "modified"
	}

	return fmt.Sprintf("%s %s %s", actorDisplay, action, entityDisplay)
}

// generateEventType generates the event type if not provided
func (b *ActivityBuilder) generateEventType() string {
	if b.eventType != "" {
		return b.eventType
	}

	// Generate from entity type and action
	entityType := strings.ToLower(b.entityType)
	action := strings.ToLower(string(b.action))

	return fmt.Sprintf("%s.%s", entityType, action)
}

// Publish publishes the activity event
func (b *ActivityBuilder) Publish() error {
	// Validate required fields
	if b.schoolID == "" {
		return fmt.Errorf("school ID is required")
	}
	if b.actorID == "" {
		return fmt.Errorf("actor ID is required")
	}
	if b.entityType == "" || b.entityID == "" {
		return fmt.Errorf("entity type and ID are required")
	}

	// Generate narrative if not provided
	narrative := b.generateNarrative()
	eventType := b.generateEventType()

	// Create the base event
	event := &Event{
		ID:            uuid.New().String(),
		AggregateID:   b.entityID,
		AggregateType: b.entityType,
		EventType:     eventType,
		EventVersion:  "1.0",
		Timestamp:     time.Now().UTC(),
		UserID:        b.actorID,
		CorrelationID: b.correlationID,
		Data: map[string]any{
			"school_id":      b.schoolID,
			"category":       string(b.category),
			"subcategory":    b.subcategory,
			"severity":       string(b.severity),
			"actor_name":     b.actorName,
			"actor_role":     b.actorRole,
			"actor_type":     b.actorType,
			"entity_name":    b.entityName,
			"action":         string(b.action),
			"narrative":      narrative,
			"narrative_html": b.narrativeHTML,
			"deep_link":      b.deepLink,
			"source":         b.source,
			"ip_address":     b.ipAddress,
			"user_agent":     b.userAgent,
		},
		Metadata: map[string]any{
			"service":    b.publisher.serviceName,
			"request_id": b.requestID,
		},
	}

	// Add changes if present
	if b.changes != nil {
		event.Data["changes"] = b.changes
	}
	if b.previousState != nil {
		event.Data["previous_state"] = b.previousState
	}
	if b.currentState != nil {
		event.Data["current_state"] = b.currentState
	}

	// Publish the event
	if err := b.publisher.bus.Publish(event); err != nil {
		b.publisher.logger.Error("Failed to publish activity event",
			logger.ErrorField(err),
			logger.String("event_type", eventType),
			logger.String("school_id", b.schoolID),
		)
		return fmt.Errorf("failed to publish activity event: %w", err)
	}

	b.publisher.logger.Debug("Activity event published",
		logger.String("event_id", event.ID),
		logger.String("event_type", eventType),
		logger.String("narrative", narrative),
	)

	return nil
}

// Common activity event types for the platform
const (
	// Subject events
	ActivitySubjectCreated         = "subject.created"
	ActivitySubjectUpdated         = "subject.updated"
	ActivitySubjectDeleted         = "subject.deleted"
	ActivitySubjectArchived        = "subject.archived"
	ActivityTeacherAssigned        = "subject.teacher_assigned"
	ActivityTeacherUnassigned      = "subject.teacher_unassigned"
	ActivityClassAssigned          = "subject.class_assigned"
	ActivityClassUnassigned        = "subject.class_unassigned"
	ActivityStudentEnrolled        = "subject.student_enrolled"
	ActivityStudentUnenrolled      = "subject.student_unenrolled"
	ActivitySubjectScheduleUpdated = "subject.schedule_updated"

	// Timetable events
	ActivityTimetableCreated     = "timetable.created"
	ActivityTimetableUpdated     = "timetable.updated"
	ActivityTimetableDeleted     = "timetable.deleted"
	ActivityTimetableSlotAdded   = "timetable.slot_added"
	ActivityTimetableSlotRemoved = "timetable.slot_removed"

	// Gradebook events
	ActivityAssessmentCreated  = "assessment.created"
	ActivityAssessmentUpdated  = "assessment.updated"
	ActivityAssessmentDeleted  = "assessment.deleted"
	ActivityGradeEntered       = "grade.entered"
	ActivityGradeUpdated       = "grade.updated"
	ActivityGradeBulkImported  = "grade.bulk_imported"
	ActivityGradebookPublished = "gradebook.published"

	// Curriculum events
	ActivityCurriculumCreated    = "curriculum.created"
	ActivityCurriculumUpdated    = "curriculum.updated"
	ActivityCurriculumDeleted    = "curriculum.deleted"
	ActivityCurriculumPublished  = "curriculum.published"
	ActivityTopicAdded           = "curriculum.topic_added"
	ActivityTopicUpdated         = "curriculum.topic_updated"
	ActivityObjectiveAdded       = "curriculum.objective_added"
	ActivityObjectiveCompleted   = "curriculum.objective_completed"

	// Student events
	ActivityStudentCreated       = "student.created"
	ActivityStudentUpdated       = "student.updated"
	ActivityStudentDeleted       = "student.deleted"
	ActivityStudentBulkImported  = "student.bulk_imported"
	ActivityStudentPromoted      = "student.promoted"
	ActivityStudentTransferred   = "student.transferred"

	// Settings events
	ActivitySchoolSettingsUpdated = "settings.school_updated"
	ActivityThemeChanged          = "settings.theme_changed"
	ActivityStructureUpdated      = "settings.structure_updated"
	ActivityCalendarUpdated       = "settings.calendar_updated"
	ActivityTermsUpdated          = "settings.terms_updated"
)
