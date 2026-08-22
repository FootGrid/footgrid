package projection

import (
	"testing"
)

func TestDecodeEventSupportsDirectOutboxMessage(t *testing.T) {
	event, err := DecodeEvent([]byte(`{"id":"11111111-1111-4111-8111-111111111111","event_type":"match.created.v1","aggregate_id":"22222222-2222-4222-8222-222222222222","payload":{"id":"22222222-2222-4222-8222-222222222222"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.SourceID == "" || event.EventType != "match.created.v1" || event.AggregateID == "" || len(event.Payload) == 0 {
		t.Fatalf("unexpected decoded event: %#v", event)
	}
}

func TestDecodeEventSupportsEventBridgeDetail(t *testing.T) {
	event, err := DecodeEvent([]byte(`{"id":"11111111-1111-4111-8111-111111111111","detail-type":"match.event-recorded.v1","detail":{"aggregate_id":"22222222-2222-4222-8222-222222222222","payload":{"snapshot":{"match_id":"22222222-2222-4222-8222-222222222222"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.EventType != "match.event-recorded.v1" || event.AggregateID == "" {
		t.Fatalf("unexpected decoded detail: %#v", event)
	}
}

func TestDecodeEventRejectsIncompleteMessage(t *testing.T) {
	if _, err := DecodeEvent([]byte(`{"id":"not-a-uuid"}`)); err == nil {
		t.Fatal("expected incomplete message error")
	}
}
