package queel

import (
	"os"
	"strings"
	"testing"
)

func TestEnginePutGetDelete(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if err := e.Put([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := e.Put([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}

	v, ok, err := e.Get([]byte("a"))
	if err != nil || !ok || string(v) != "1" {
		t.Fatalf("Get(a) = %q, %v, %v", v, ok, err)
	}

	if err := e.Delete([]byte("a")); err != nil {
		t.Fatal(err)
	}
	_, ok, err = e.Get([]byte("a"))
	if err != nil || ok {
		t.Fatalf("expected deleted key to be absent, got ok=%v err=%v", ok, err)
	}

	v, ok, err = e.Get([]byte("b"))
	if err != nil || !ok || string(v) != "2" {
		t.Fatalf("Get(b) = %q, %v, %v", v, ok, err)
	}
}

func TestEngineFlushToSSTable(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.memtableSizeLimit = 1 // force an immediate flush

	if err := e.Put([]byte("x"), []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if len(e.sstables) != 1 {
		t.Fatalf("expected 1 sstable after flush, got %d", len(e.sstables))
	}
	if e.memtable.Size() != 0 {
		t.Fatalf("expected memtable to be reset after flush, size=%d", e.memtable.Size())
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: the value must still be readable, now served from the SSTable.
	e2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()

	v, ok, err := e2.Get([]byte("x"))
	if err != nil || !ok || string(v) != "hello" {
		t.Fatalf("Get(x) after reopen = %q, %v, %v", v, ok, err)
	}
}

func TestEngineRecoversFromWALWithoutCleanClose(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash: drop the engine without a clean Close/flush.
	_ = e.wal.file.Close()

	e2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()

	v, ok, err := e2.Get([]byte("k"))
	if err != nil || !ok || string(v) != "v" {
		t.Fatalf("expected WAL-recovered value, got %q, %v, %v", v, ok, err)
	}
}

func TestEngineNewerSSTableShadowsOlder(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.memtableSizeLimit = 1

	if err := e.Put([]byte("k"), []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := e.Put([]byte("k"), []byte("new")); err != nil {
		t.Fatal(err)
	}

	if len(e.sstables) != 2 {
		t.Fatalf("expected 2 sstables, got %d", len(e.sstables))
	}

	v, ok, err := e.Get([]byte("k"))
	if err != nil || !ok || string(v) != "new" {
		t.Fatalf("expected newest value to win, got %q, %v, %v", v, ok, err)
	}
}

func TestEngineCompactMergesAndDropsShadowedTombstones(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.memtableSizeLimit = 1 // force a flush after every write

	if err := e.Put([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := e.Put([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}
	if err := e.Put([]byte("a"), []byte("1-updated")); err != nil {
		t.Fatal(err)
	}
	if err := e.Delete([]byte("b")); err != nil {
		t.Fatal(err)
	}

	if len(e.sstables) != 4 {
		t.Fatalf("expected 4 sstables before compaction, got %d", len(e.sstables))
	}

	if err := e.Compact(); err != nil {
		t.Fatal(err)
	}

	if len(e.sstables) != 1 {
		t.Fatalf("expected 1 sstable after compaction, got %d", len(e.sstables))
	}

	v, ok, err := e.Get([]byte("a"))
	if err != nil || !ok || string(v) != "1-updated" {
		t.Fatalf("Get(a) after compaction = %q, %v, %v", v, ok, err)
	}

	if _, ok, err := e.Get([]byte("b")); err != nil || ok {
		t.Fatalf("expected b to remain deleted after compaction, ok=%v err=%v", ok, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	sstableFiles := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "sstable-") {
			sstableFiles++
		}
	}
	if sstableFiles != 1 {
		t.Fatalf("expected exactly 1 sstable file on disk, found %d", sstableFiles)
	}
}
