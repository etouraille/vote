package cluster

import "time"

// Entry is what a node actually stores locally for a replicated key: the
// raw value plus a timestamp used to resolve conflicting replicas via
// last-write-wins, and a tombstone flag for replicated deletes.
type Entry struct {
	Value     []byte `json:"value,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Tombstone bool   `json:"tombstone,omitempty"`
}

// NewEntry stamps value with the current time, ready to be replicated.
func NewEntry(value []byte, tombstone bool) Entry {
	return Entry{Value: value, Timestamp: time.Now().UnixNano(), Tombstone: tombstone}
}
