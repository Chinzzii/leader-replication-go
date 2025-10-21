// internal/store/store.go
package store

import (
	"sync"
	"time"
)

// Entry represents a single key-value pair with a timestamp.
// The timestamp is used for conflict resolution (Last Write Wins).
type Entry struct {
	Key   string    `json:"key"`
	Value string    `json:"value"`
	TS    time.Time `json:"ts"` // TS is the timestamp of the write
}

// KV provides a thread-safe, in-memory key-value store.
// It uses a RWMutex to protect concurrent access to the underlying map.
type KV struct {
	mu   sync.RWMutex     // mu protects the data map
	data map[string]Entry // data stores all key-value entries
}

// New creates and initializes a new KV store.
func New() *KV {
	return &KV{
		data: make(map[string]Entry),
		// The mu (RWMutex) is usable at its zero value.
	}
}

// Upsert adds or updates an entry in the store.
// It uses a Last-Write-Wins (LWW) conflict resolution strategy:
// The entry is only updated if the new entry's timestamp (e.TS)
// is greater than or equal to the existing entry's timestamp.
func (kv *KV) Upsert(e Entry) {
	kv.mu.Lock() // Acquire a full write lock
	defer kv.mu.Unlock()

	cur, ok := kv.data[e.Key]

	// Update if:
	// 1. The key doesn't exist (!ok)
	// 2. The new timestamp is later (e.TS.After)
	// 3. The new timestamp is the same (e.TS.Equal)
	if !ok || !e.TS.Before(cur.TS) { // Simplified from (e.TS.After || e.TS.Equal)
		kv.data[e.Key] = e
	}
	// If the new timestamp is *before* the current one, we ignore it.
}

// Get retrieves an entry from the store.
// It returns the entry and true if the key exists,
// or an empty Entry and false if it does not.
func (kv *KV) Get(key string) (Entry, bool) {
	kv.mu.RLock() // Acquire a read lock (allows other concurrent reads)
	defer kv.mu.RUnlock()

	e, ok := kv.data[key]
	return e, ok
}

// Snapshot returns a shallow copy of the entire data map.
// This is used for operations (like status endpoints or bulk replication)
// that need a consistent view of the data without holding a lock
// for an extended period.
func (kv *KV) Snapshot() map[string]Entry {
	kv.mu.RLock() // Acquire a read lock
	defer kv.mu.RUnlock()

	// Create a new map with the same initial capacity.
	copy := make(map[string]Entry, len(kv.data))
	// Iterate and copy each entry.
	for k, v := range kv.data {
		copy[k] = v
	}
	return copy
}
