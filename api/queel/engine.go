package queel

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const defaultMemtableSizeLimit = 4 * 1024 * 1024 // 4MB

// Engine is a single-node LSM-tree key/value store: writes land in the WAL
// then the memtable; once the memtable grows past memtableSizeLimit it is
// flushed to a new immutable SSTable on disk. Reads check the memtable first,
// then SSTables from newest to oldest, so newer writes always shadow older ones.
type Engine struct {
	mu                sync.RWMutex
	dataDir           string
	wal               *WAL
	memtable          *Memtable
	sstables          []*SSTable // newest first
	nextSSTableSeq    int
	memtableSizeLimit int
}

// Open creates dataDir if needed, reloads any existing SSTables, and replays
// the write-ahead log to recover writes that hadn't been flushed yet.
func Open(dataDir string) (*Engine, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	e := &Engine{
		dataDir:           dataDir,
		memtable:          NewMemtable(),
		memtableSizeLimit: defaultMemtableSizeLimit,
	}

	sstablePaths, err := existingSSTables(dataDir)
	if err != nil {
		return nil, err
	}
	for _, p := range sstablePaths { // oldest -> newest
		sst, err := OpenSSTable(p)
		if err != nil {
			return nil, err
		}
		e.sstables = append([]*SSTable{sst}, e.sstables...) // keep newest-first
	}
	e.nextSSTableSeq = len(sstablePaths)

	walPath := filepath.Join(dataDir, "wal.log")
	if err := ReplayWAL(walPath, func(key, value []byte, tombstone bool) error {
		e.memtable.Put(key, value, tombstone)
		return nil
	}); err != nil {
		return nil, err
	}

	wal, err := OpenWAL(walPath)
	if err != nil {
		return nil, err
	}
	e.wal = wal

	return e, nil
}

func existingSSTables(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}

	var seqs []int
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "sstable-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		seqStr := strings.TrimSuffix(strings.TrimPrefix(name, "sstable-"), ".db")
		seq, err := strconv.Atoi(seqStr)
		if err != nil {
			continue
		}
		seqs = append(seqs, seq)
	}
	sort.Ints(seqs)

	paths := make([]string, len(seqs))
	for i, seq := range seqs {
		paths[i] = filepath.Join(dataDir, fmt.Sprintf("sstable-%d.db", seq))
	}
	return paths, nil
}

// Put durably writes key/value: WAL first, then the memtable.
func (e *Engine) Put(key, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.wal.Append(key, value, false); err != nil {
		return err
	}
	e.memtable.Put(key, value, false)
	return e.maybeFlush()
}

// Delete records a tombstone for key so it stops shadowing through to older
// SSTable values, without needing to rewrite them immediately.
func (e *Engine) Delete(key []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.wal.Append(key, nil, true); err != nil {
		return err
	}
	e.memtable.Put(key, nil, true)
	return e.maybeFlush()
}

// WriteBatch applies each op in order via Put or Delete. A local Engine has
// no network round trips to save, so this is just a loop — the payoff is
// over a distributed Store, which this same call satisfies too.
func (e *Engine) WriteBatch(ops []WriteOp) error {
	for _, op := range ops {
		if op.Tombstone {
			if err := e.Delete(op.Key); err != nil {
				return err
			}
			continue
		}
		if err := e.Put(op.Key, op.Value); err != nil {
			return err
		}
	}
	return nil
}

// Get returns the current value for key, checking the memtable then each
// SSTable from newest to oldest; a tombstone at any level means "not found".
func (e *Engine) Get(key []byte) ([]byte, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if value, tombstone, found := e.memtable.Get(key); found {
		if tombstone {
			return nil, false, nil
		}
		return value, true, nil
	}

	for _, sst := range e.sstables {
		value, tombstone, found, err := sst.Get(key)
		if err != nil {
			return nil, false, err
		}
		if found {
			if tombstone {
				return nil, false, nil
			}
			return value, true, nil
		}
	}

	return nil, false, nil
}

// KV is one key/value pair returned by Scan.
type KV struct {
	Key   []byte
	Value []byte
}

// Scan returns every live (non-tombstoned) key/value pair whose key starts
// with prefix, in ascending key order. Like Get, a more recent source
// (memtable, then newer SSTables) shadows older ones for the same key.
//
// This reads every SSTable in full on each call rather than seeking with the
// index, which is fine at this stage but is the first thing to optimize once
// scans need to be fast over large datasets.
func (e *Engine) Scan(prefix []byte) ([]KV, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	latest := make(map[string]memtableEntry)

	for i := len(e.sstables) - 1; i >= 0; i-- { // oldest -> newest, so newest wins
		entries, err := e.sstables[i].Entries()
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			latest[string(entry.key)] = entry
		}
	}
	for _, entry := range e.memtable.Entries() {
		latest[string(entry.key)] = entry
	}

	result := make([]KV, 0, len(latest))
	for k, entry := range latest {
		if entry.tombstone || !strings.HasPrefix(k, string(prefix)) {
			continue
		}
		result = append(result, KV{Key: entry.key, Value: entry.value})
	}
	sort.Slice(result, func(i, j int) bool { return bytes.Compare(result[i].Key, result[j].Key) < 0 })
	return result, nil
}

func (e *Engine) maybeFlush() error {
	if e.memtable.Size() < e.memtableSizeLimit {
		return nil
	}

	path := filepath.Join(e.dataDir, fmt.Sprintf("sstable-%d.db", e.nextSSTableSeq))
	sst, err := WriteSSTable(path, e.memtable.Entries())
	if err != nil {
		return err
	}
	e.nextSSTableSeq++
	e.sstables = append([]*SSTable{sst}, e.sstables...)
	e.memtable = NewMemtable()
	return e.wal.Reset()
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.wal.Close()
}
