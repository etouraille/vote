package queel

import (
	"bytes"
	"math/rand"
)

const maxSkipListLevel = 16
const skipListP = 0.5

type skipListNode struct {
	key       []byte
	value     []byte
	tombstone bool
	forward   []*skipListNode
}

// Memtable is the in-memory, sorted write buffer of the LSM-tree. It is
// implemented as a skip list so inserts and lookups stay O(log n) while
// keeping keys in sorted order, ready to be flushed to an SSTable.
type Memtable struct {
	head       *skipListNode
	level      int
	size       int
	entryCount int
}

func NewMemtable() *Memtable {
	return &Memtable{
		head:  &skipListNode{forward: make([]*skipListNode, maxSkipListLevel)},
		level: 1,
	}
}

func (m *Memtable) randomLevel() int {
	level := 1
	for level < maxSkipListLevel && rand.Float64() < skipListP {
		level++
	}
	return level
}

// Put inserts or overwrites key. tombstone marks the key as deleted while
// still keeping a record of it, so a Delete can shadow older SSTable values.
func (m *Memtable) Put(key, value []byte, tombstone bool) {
	update := make([]*skipListNode, maxSkipListLevel)
	current := m.head
	for i := m.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && bytes.Compare(current.forward[i].key, key) < 0 {
			current = current.forward[i]
		}
		update[i] = current
	}
	current = current.forward[0]

	if current != nil && bytes.Equal(current.key, key) {
		m.size += len(value) - len(current.value)
		current.value = append([]byte(nil), value...)
		current.tombstone = tombstone
		return
	}

	newLevel := m.randomLevel()
	if newLevel > m.level {
		for i := m.level; i < newLevel; i++ {
			update[i] = m.head
		}
		m.level = newLevel
	}

	node := &skipListNode{
		key:       append([]byte(nil), key...),
		value:     append([]byte(nil), value...),
		tombstone: tombstone,
		forward:   make([]*skipListNode, newLevel),
	}
	for i := 0; i < newLevel; i++ {
		node.forward[i] = update[i].forward[i]
		update[i].forward[i] = node
	}
	m.size += len(key) + len(value)
	m.entryCount++
}

// Get returns the value stored for key, whether it is a tombstone, and
// whether the key was found at all in this memtable.
func (m *Memtable) Get(key []byte) (value []byte, tombstone bool, found bool) {
	current := m.head
	for i := m.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && bytes.Compare(current.forward[i].key, key) < 0 {
			current = current.forward[i]
		}
	}
	current = current.forward[0]
	if current != nil && bytes.Equal(current.key, key) {
		return current.value, current.tombstone, true
	}
	return nil, false, false
}

// Size is the approximate memory footprint of the memtable, in bytes.
func (m *Memtable) Size() int {
	return m.size
}

type memtableEntry struct {
	key       []byte
	value     []byte
	tombstone bool
}

// Entries returns every entry in ascending key order, ready to be written
// out to an SSTable.
func (m *Memtable) Entries() []memtableEntry {
	entries := make([]memtableEntry, 0, m.entryCount)
	for node := m.head.forward[0]; node != nil; node = node.forward[0] {
		entries = append(entries, memtableEntry{key: node.key, value: node.value, tombstone: node.tombstone})
	}
	return entries
}
