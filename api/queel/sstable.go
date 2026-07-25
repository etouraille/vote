package queel

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"sort"
)

// tombstoneMarker is stored in place of a value length to mark a deleted key.
const tombstoneMarker uint32 = 0xFFFFFFFF

type ssTableIndexEntry struct {
	key    []byte
	offset int64
}

// SSTable is an immutable, sorted, on-disk run of key/value records produced
// by flushing a memtable. Its index (key -> file offset) is rebuilt in
// memory whenever the file is opened, so lookups only need one disk seek.
type SSTable struct {
	path  string
	index []ssTableIndexEntry
}

// WriteSSTable sorts entries by key and writes them to a new file at path.
func WriteSSTable(path string, entries []memtableEntry) (*SSTable, error) {
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	index := make([]ssTableIndexEntry, 0, len(entries))
	var offset int64

	for _, e := range entries {
		valueLen := uint32(len(e.value))
		if e.tombstone {
			valueLen = tombstoneMarker
		}
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], uint32(len(e.key)))
		binary.BigEndian.PutUint32(header[4:8], valueLen)

		index = append(index, ssTableIndexEntry{key: e.key, offset: offset})

		n1, err := w.Write(header)
		if err != nil {
			return nil, err
		}
		n2, err := w.Write(e.key)
		if err != nil {
			return nil, err
		}
		n3 := 0
		if !e.tombstone {
			n3, err = w.Write(e.value)
			if err != nil {
				return nil, err
			}
		}
		offset += int64(n1 + n2 + n3)
	}

	if err := w.Flush(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}

	return &SSTable{path: path, index: index}, nil
}

// OpenSSTable opens an existing SSTable file and rebuilds its in-memory index
// by scanning it once.
func OpenSSTable(path string) (*SSTable, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var index []ssTableIndexEntry
	var offset int64
	header := make([]byte, 8)

	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		keyLen := binary.BigEndian.Uint32(header[0:4])
		valueLen := binary.BigEndian.Uint32(header[4:8])

		key := make([]byte, keyLen)
		if _, err := io.ReadFull(r, key); err != nil {
			return nil, err
		}
		index = append(index, ssTableIndexEntry{key: key, offset: offset})

		recordLen := int64(8 + len(key))
		if valueLen != tombstoneMarker {
			if _, err := io.CopyN(io.Discard, r, int64(valueLen)); err != nil {
				return nil, err
			}
			recordLen += int64(valueLen)
		}
		offset += recordLen
	}

	return &SSTable{path: path, index: index}, nil
}

// Entries reads every record in the SSTable back out, in sorted file order.
// Used by compaction to merge several SSTables into one.
func (s *SSTable) Entries() ([]memtableEntry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var entries []memtableEntry
	header := make([]byte, 8)

	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		keyLen := binary.BigEndian.Uint32(header[0:4])
		valueLen := binary.BigEndian.Uint32(header[4:8])

		key := make([]byte, keyLen)
		if _, err := io.ReadFull(r, key); err != nil {
			return nil, err
		}

		if valueLen == tombstoneMarker {
			entries = append(entries, memtableEntry{key: key, tombstone: true})
			continue
		}

		value := make([]byte, valueLen)
		if _, err := io.ReadFull(r, value); err != nil {
			return nil, err
		}
		entries = append(entries, memtableEntry{key: key, value: value})
	}

	return entries, nil
}

// Get looks up key via a binary search over the in-memory index, then reads
// the single matching record straight off disk.
func (s *SSTable) Get(key []byte) (value []byte, tombstone bool, found bool, err error) {
	i := sort.Search(len(s.index), func(i int) bool { return bytes.Compare(s.index[i].key, key) >= 0 })
	if i >= len(s.index) || !bytes.Equal(s.index[i].key, key) {
		return nil, false, false, nil
	}

	f, err := os.Open(s.path)
	if err != nil {
		return nil, false, false, err
	}
	defer f.Close()

	if _, err := f.Seek(s.index[i].offset, io.SeekStart); err != nil {
		return nil, false, false, err
	}

	header := make([]byte, 8)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, false, false, err
	}
	keyLen := binary.BigEndian.Uint32(header[0:4])
	valueLen := binary.BigEndian.Uint32(header[4:8])

	if _, err := f.Seek(int64(keyLen), io.SeekCurrent); err != nil {
		return nil, false, false, err
	}

	if valueLen == tombstoneMarker {
		return nil, true, true, nil
	}

	value = make([]byte, valueLen)
	if _, err := io.ReadFull(f, value); err != nil {
		return nil, false, false, err
	}
	return value, false, true, nil
}
