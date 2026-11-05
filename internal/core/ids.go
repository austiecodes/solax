package core

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// ID is a prefixed, time-sortable object ID, for example "tsk_0m3k2f9a1b2c3d4e".
// Each injected object includes the IDs of related objects (Chain of Recall).
type ID string

// Prefix is the type prefix of an ID.
type Prefix string

const (
	PrefixMachine   Prefix = "mch"
	PrefixAgent     Prefix = "agt"
	PrefixWorkspace Prefix = "ws"
	PrefixTask      Prefix = "tsk"
	PrefixMessage   Prefix = "msg"
	PrefixSession   Prefix = "ses"
	PrefixBatch     Prefix = "bat"
)

// NewID returns a new ID: the prefix, the time in base 36 milliseconds,
// and 8 random bytes. IDs of one prefix sort by creation time.
func NewID(p Prefix) ID {
	var b [8]byte
	_, _ = rand.Read(b[:])
	ts := strconv.FormatInt(time.Now().UnixMilli(), 36)
	return ID(string(p) + "_" + ts + hex.EncodeToString(b[:]))
}

// Prefix returns the type prefix of the ID, or "" when it has none.
func (id ID) Prefix() Prefix {
	p, _, ok := strings.Cut(string(id), "_")
	if !ok {
		return ""
	}
	return Prefix(p)
}
