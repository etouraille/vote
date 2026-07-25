package queel

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
)

// WAL is the write-ahead log: every write is appended and fsynced here
// before it touches the memtable, so a crash can never lose an acknowledged
// write — replaying this file rebuilds whatever the memtable hadn't flushed.
type WAL struct {
	file *os.File
	w    *bufio.Writer
}

func OpenWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: f, w: bufio.NewWriter(f)}, nil
}

// Append writes one record: a fixed header (key length, value length,
// tombstone flag) followed by the raw key and value bytes.
func (w *WAL) Append(key, value []byte, tombstone bool) error {
	var tomb byte
	if tombstone {
		tomb = 1
	}
	header := make([]byte, 9)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(key)))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(value)))
	header[8] = tomb

	if _, err := w.w.Write(header); err != nil {
		return err
	}
	if _, err := w.w.Write(key); err != nil {
		return err
	}
	if _, err := w.w.Write(value); err != nil {
		return err
	}
	if err := w.w.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Reset truncates the log after its contents have been safely flushed to a
// new SSTable.
func (w *WAL) Reset() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	_, err := w.file.Seek(0, io.SeekStart)
	return err
}

func (w *WAL) Close() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	return w.file.Close()
}

// ReplayWAL reads every record from the log at path, in write order, calling
// fn for each one. A missing file just means there was nothing to recover.
func ReplayWAL(path string, fn func(key, value []byte, tombstone bool) error) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	header := make([]byte, 9)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		keyLen := binary.BigEndian.Uint32(header[0:4])
		valueLen := binary.BigEndian.Uint32(header[4:8])
		tombstone := header[8] == 1

		key := make([]byte, keyLen)
		if _, err := io.ReadFull(r, key); err != nil {
			return err
		}
		value := make([]byte, valueLen)
		if _, err := io.ReadFull(r, value); err != nil {
			return err
		}
		if err := fn(key, value, tombstone); err != nil {
			return err
		}
	}
	return nil
}
