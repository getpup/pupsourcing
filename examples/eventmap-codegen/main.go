//go:generate go run ../../cmd/eventmap-gen -input domain/user/events -output infrastructure/persistence -package persistence -module github.com/getpup/pupsourcing/examples/eventmap-codegen/domain/user/events

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/examples/eventmap-codegen/domain/user/events/v1"
	"github.com/getpup/pupsourcing/examples/eventmap-codegen/domain/user/events/v2"
	"github.com/getpup/pupsourcing/examples/eventmap-codegen/infrastructure/persistence"
	"github.com/google/uuid"
)

func main() {
	fmt.Println("Event Mapping Code Generation Example")
	fmt.Println("======================================")
	fmt.Println()

	// Example 1: Convert v1 domain events to ES events
	fmt.Println("Example 1: Domain Events V1 → ES Events")
	fmt.Println("----------------------------------------")
	
	v1Events := []any{
		v1.UserRegistered{
			Email: "alice@example.com",
			Name:  "Alice Smith",
		},
		v1.UserEmailChanged{
			OldEmail: "alice@example.com",
			NewEmail: "alice.smith@example.com",
		},
	}

	userID := uuid.New().String()
	
	esEvents, err := persistence.ToESEvents(
		"User",
		userID,
		v1Events,
		persistence.WithTraceID("trace-123"),
		persistence.WithCorrelationID("corr-456"),
	)
	if err != nil {
		log.Fatalf("Failed to convert v1 events: %v", err)
	}

	for i, event := range esEvents {
		fmt.Printf("Event %d:\n", i+1)
		fmt.Printf("  Type: %s\n", event.EventType)
		fmt.Printf("  Version: %d\n", event.EventVersion)
		fmt.Printf("  AggregateID: %s\n", event.AggregateID)
		fmt.Printf("  Payload: %s\n", string(event.Payload))
		if event.TraceID.Valid {
			fmt.Printf("  TraceID: %s\n", event.TraceID.String)
		}
		fmt.Println()
	}

	// Example 2: Convert v2 domain event to ES event
	fmt.Println("Example 2: Domain Event V2 → ES Event")
	fmt.Println("--------------------------------------")
	
	v2Event := v2.UserRegistered{
		Email:        "bob@example.com",
		Name:         "Bob Johnson",
		Country:      "USA",
		RegisteredAt: time.Now().Unix(),
	}

	esEvent, err := persistence.ToUserRegisteredV2(
		"User",
		uuid.New().String(),
		v2Event,
		persistence.WithCausationID("cmd-789"),
	)
	if err != nil {
		log.Fatalf("Failed to convert v2 event: %v", err)
	}

	fmt.Printf("Event:\n")
	fmt.Printf("  Type: %s\n", esEvent.EventType)
	fmt.Printf("  Version: %d\n", esEvent.EventVersion)
	fmt.Printf("  Payload: %s\n", string(esEvent.Payload))
	if esEvent.CausationID.Valid {
		fmt.Printf("  CausationID: %s\n", esEvent.CausationID.String)
	}
	fmt.Println()

	// Example 3: Simulate persisted events and convert back to domain
	fmt.Println("Example 3: ES Events → Domain Events (Round Trip)")
	fmt.Println("--------------------------------------------------")

	// Simulate events stored in database (mixed versions)
	persistedEvents := []es.PersistedEvent{
		{
			AggregateType:    "User",
			AggregateID:      userID,
			EventType:        "UserRegistered",
			EventVersion:     1,
			EventID:          uuid.New(),
			Payload:          []byte(`{"email":"charlie@example.com","name":"Charlie Brown"}`),
			Metadata:         []byte("{}"),
			GlobalPosition:   1,
			AggregateVersion: 1,
			CreatedAt:        time.Now(),
		},
		{
			AggregateType:    "User",
			AggregateID:      userID,
			EventType:        "UserEmailChanged",
			EventVersion:     1,
			EventID:          uuid.New(),
			Payload:          []byte(`{"old_email":"charlie@example.com","new_email":"charlie.brown@example.com"}`),
			Metadata:         []byte("{}"),
			GlobalPosition:   2,
			AggregateVersion: 2,
			CreatedAt:        time.Now(),
		},
		{
			AggregateType:    "User",
			AggregateID:      userID,
			EventType:        "UserRegistered",
			EventVersion:     2,
			EventID:          uuid.New(),
			Payload:          []byte(`{"email":"dave@example.com","name":"Dave Wilson","country":"Canada","registered_at":1704067200}`),
			Metadata:         []byte("{}"),
			GlobalPosition:   3,
			AggregateVersion: 1,
			CreatedAt:        time.Now(),
		},
	}

	// Convert persisted events back to domain events
	domainEvents, err := persistence.FromESEvents[any](persistedEvents)
	if err != nil {
		log.Fatalf("Failed to convert persisted events: %v", err)
	}

	for i, event := range domainEvents {
		fmt.Printf("Restored Event %d:\n", i+1)
		switch e := event.(type) {
		case v1.UserRegistered:
			fmt.Printf("  Type: UserRegistered (v1)\n")
			fmt.Printf("  Email: %s\n", e.Email)
			fmt.Printf("  Name: %s\n", e.Name)
		case v1.UserEmailChanged:
			fmt.Printf("  Type: UserEmailChanged (v1)\n")
			fmt.Printf("  OldEmail: %s\n", e.OldEmail)
			fmt.Printf("  NewEmail: %s\n", e.NewEmail)
		case v2.UserRegistered:
			fmt.Printf("  Type: UserRegistered (v2)\n")
			fmt.Printf("  Email: %s\n", e.Email)
			fmt.Printf("  Name: %s\n", e.Name)
			fmt.Printf("  Country: %s\n", e.Country)
			fmt.Printf("  RegisteredAt: %d\n", e.RegisteredAt)
		default:
			fmt.Printf("  Type: %T (unknown)\n", e)
		}
		fmt.Println()
	}

	// Example 4: Type-safe conversion with specific helpers
	fmt.Println("Example 4: Type-Safe Helper Functions")
	fmt.Println("--------------------------------------")

	specificEvent := v1.UserRegistered{
		Email: "eve@example.com",
		Name:  "Eve Martinez",
	}

	// Use type-safe helper
	specificESEvent, err := persistence.ToUserRegisteredV1(
		"User",
		uuid.New().String(),
		specificEvent,
	)
	if err != nil {
		log.Fatalf("Failed to convert with type-safe helper: %v", err)
	}

	fmt.Printf("Converted with ToUserRegisteredV1:\n")
	fmt.Printf("  EventType: %s\n", specificESEvent.EventType)
	fmt.Printf("  EventVersion: %d\n", specificESEvent.EventVersion)
	fmt.Println()

	// Convert back with type-safe helper
	persistedSpecific := es.PersistedEvent{
		AggregateType:    specificESEvent.AggregateType,
		AggregateID:      specificESEvent.AggregateID,
		EventType:        specificESEvent.EventType,
		EventVersion:     specificESEvent.EventVersion,
		EventID:          specificESEvent.EventID,
		Payload:          specificESEvent.Payload,
		Metadata:         specificESEvent.Metadata,
		GlobalPosition:   1,
		AggregateVersion: 1,
		CreatedAt:        specificESEvent.CreatedAt,
	}

	restoredSpecific, err := persistence.FromUserRegisteredV1(persistedSpecific)
	if err != nil {
		log.Fatalf("Failed to restore with type-safe helper: %v", err)
	}

	fmt.Printf("Restored with FromUserRegisteredV1:\n")
	fmt.Printf("  Email: %s\n", restoredSpecific.Email)
	fmt.Printf("  Name: %s\n", restoredSpecific.Name)
	fmt.Println()

	fmt.Println("✓ All examples completed successfully!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Modify domain events in domain/user/events/")
	fmt.Println("  2. Run: go generate")
	fmt.Println("  3. Run: go run .")
}
