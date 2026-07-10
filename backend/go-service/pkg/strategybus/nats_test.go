package strategybus

import (
	"testing"
	"time"
)

func TestNormalizeNATSOptionsDefaults(t *testing.T) {
	options := normalizeNATSOptions(NATSOptions{})
	if options.URL != DefaultNATSURL {
		t.Fatalf("url = %q, want %q", options.URL, DefaultNATSURL)
	}
	if options.Stream != DefaultDecisionStreamName {
		t.Fatalf("stream = %q, want %q", options.Stream, DefaultDecisionStreamName)
	}
	if options.Subject != DefaultDecisionSubject {
		t.Fatalf("subject = %q, want %q", options.Subject, DefaultDecisionSubject)
	}
	if options.Durable != "position-engine" {
		t.Fatalf("durable = %q, want position-engine", options.Durable)
	}
	if options.Consumer != options.Durable {
		t.Fatalf("consumer = %q, want durable %q", options.Consumer, options.Durable)
	}
	if options.AckWait <= 0 {
		t.Fatalf("ack wait = %s, want positive", options.AckWait)
	}
	if options.DeadLetterSubject != DefaultDecisionSubject+".dead" {
		t.Fatalf("dead letter subject = %q, want default", options.DeadLetterSubject)
	}
	if options.MaxDeliveries != 5 {
		t.Fatalf("max deliveries = %d, want 5", options.MaxDeliveries)
	}
}

func TestNormalizeNATSOptionsPreservesValues(t *testing.T) {
	options := normalizeNATSOptions(NATSOptions{
		URL:               "nats://example:4222",
		Stream:            "CUSTOM",
		Subject:           "custom.subject",
		Durable:           "custom-durable",
		Consumer:          "custom-consumer",
		Block:             time.Second,
		Batch:             20,
		AckWait:           time.Minute,
		MaxDeliveries:     9,
		DeadLetterSubject: "custom.subject.dead",
	})
	if options.URL != "nats://example:4222" {
		t.Fatalf("url = %q, want custom", options.URL)
	}
	if options.Stream != "CUSTOM" || options.Subject != "custom.subject" {
		t.Fatalf("stream/subject = %q/%q, want custom", options.Stream, options.Subject)
	}
	if options.Durable != "custom-durable" || options.Consumer != "custom-consumer" {
		t.Fatalf("durable/consumer = %q/%q, want custom", options.Durable, options.Consumer)
	}
	if options.Block != time.Second || options.Batch != 20 || options.AckWait != time.Minute {
		t.Fatalf("timing/batch options = %#v, want preserved", options)
	}
	if options.MaxDeliveries != 9 || options.DeadLetterSubject != "custom.subject.dead" {
		t.Fatalf("retry options = %#v, want preserved", options)
	}
}

func TestUniqueSubjects(t *testing.T) {
	subjects := uniqueSubjects("strategy.decision", "", " strategy.decision ", "strategy.decision.dead")
	if len(subjects) != 2 {
		t.Fatalf("subjects = %#v, want 2 unique subjects", subjects)
	}
	if subjects[0] != "strategy.decision" || subjects[1] != "strategy.decision.dead" {
		t.Fatalf("subjects = %#v, want order-preserved unique subjects", subjects)
	}
}

func TestDecisionStreamConfigExpiresMessages(t *testing.T) {
	config := decisionStreamConfig("STRATEGY", []string{"strategy.decision"})
	if config.MaxAge != 24*time.Hour {
		t.Fatalf("max age = %s, want %s", config.MaxAge, 24*time.Hour)
	}
}
