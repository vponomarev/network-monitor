package events

import (
	"time"
)

// EventType represents the type of event
type EventType string

// Event represents a monitoring event
type Event struct {
	Type      EventType
	Timestamp time.Time
	Source    string
	Data      interface{}
}
