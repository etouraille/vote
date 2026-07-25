package rbac

import (
	"crypto/rand"
	"fmt"
)

// newUUID generates a random UUID (v4, RFC 4122) — no external dependency,
// consistent with the rest of queel's "just crypto/rand + encoding/hex"
// ID generation (see newID in repository.go).
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
