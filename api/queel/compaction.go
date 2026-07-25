package queel

import (
	"fmt"
	"os"
	"path/filepath"
)

// Compact merges every current SSTable into a single new one, keeping only
// the newest value per key. Tombstones are dropped entirely: after a full
// compaction there is no older SSTable left for them to shadow, so they've
// done their job. Callers decide when to run this (e.g. periodically, or
// once the number of SSTables crosses some threshold) — Compact itself is
// just the merge.
func (e *Engine) Compact() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.sstables) < 2 {
		return nil
	}

	merged, err := mergeSSTables(e.sstables)
	if err != nil {
		return err
	}

	path := filepath.Join(e.dataDir, fmt.Sprintf("sstable-%d.db", e.nextSSTableSeq))
	newSST, err := WriteSSTable(path, merged)
	if err != nil {
		return err
	}
	e.nextSSTableSeq++

	oldPaths := make([]string, len(e.sstables))
	for i, sst := range e.sstables {
		oldPaths[i] = sst.path
	}
	e.sstables = []*SSTable{newSST}

	for _, p := range oldPaths {
		if err := os.Remove(p); err != nil {
			return err
		}
	}

	return nil
}

// mergeSSTables combines entries from sstables (given newest-first) into a
// deduplicated set keyed by key, the newest write winning, then drops
// tombstones since nothing older survives this merge.
func mergeSSTables(sstables []*SSTable) ([]memtableEntry, error) {
	latest := make(map[string]memtableEntry)

	for i := len(sstables) - 1; i >= 0; i-- { // oldest -> newest, so newest overwrites
		entries, err := sstables[i].Entries()
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			latest[string(e.key)] = e
		}
	}

	merged := make([]memtableEntry, 0, len(latest))
	for _, e := range latest {
		if e.tombstone {
			continue
		}
		merged = append(merged, e)
	}
	return merged, nil
}
