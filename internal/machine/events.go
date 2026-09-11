package machine

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

// Event types surfaced on the events stream.
const (
	EventMachine = "machine"
	EventUpdate  = "update"
	EventK0s     = "k0s"
)

// NodeEvent is one curated node event.
type NodeEvent struct {
	ID          string
	TimestampNs int64
	Type        string
	Message     string
	Metadata    map[string]string
}

var eventSeq atomic.Uint64

// eventKindFor maps a systemd unit / kernel identifier to a curated event
// type, or "" when the record is not interesting.
func eventKindFor(unit string) string {
	u := strings.ToLower(unit)
	switch {
	case strings.Contains(u, "k0s"):
		return EventK0s
	case strings.Contains(u, "sysupdate"), strings.Contains(u, "sysext"),
		strings.Contains(u, "boot-update"), strings.Contains(u, "boot-"):
		return EventUpdate
	case strings.Contains(u, "apxd"), strings.Contains(u, "kernel"), strings.Contains(u, "core"):
		return EventMachine
	}
	return ""
}

// EventPredicate selects journal records interesting enough to become events:
// anything at or above ELOG_PRI_ERR, plus records from our curated units.
func EventPredicate(e JournalEntry) bool {
	if e.Priority <= 3 {
		return true
	}
	return eventKindFor(e.Unit) != ""
}

// streamJournalInternal is the journal source seam (tests swap it).
var streamJournalInternal = StreamJournal

// StreamEvents tails the journal and maps matching records to NodeEvents.
func StreamEvents(ctx context.Context, emit func(NodeEvent) error) error {
	return streamJournalInternal(ctx, JournalOptions{Follow: true, TailLines: 50}, func(e JournalEntry) error {
		if !EventPredicate(e) {
			return nil
		}
		kind := eventKindFor(e.Unit)
		if kind == "" {
			kind = EventMachine
		}
		ev := NodeEvent{
			ID:          fmt.Sprintf("%d-%d", e.Timestamp.UnixNano(), eventSeq.Add(1)),
			TimestampNs: e.Timestamp.UnixNano(),
			Type:        kind,
			Message:     e.Message,
			Metadata: map[string]string{
				"unit":     e.Unit,
				"ident":    e.Identifier,
				"hostname": e.Hostname,
			},
		}
		return emit(ev)
	})
}
