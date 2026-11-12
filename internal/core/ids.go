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
	PrefixOwner     Prefix = "own"
	PrefixClient    Prefix = "cli"
	PrefixMachine   Prefix = "mch"
	PrefixAgent     Prefix = "agt"
	PrefixWorkspace Prefix = "ws"
	PrefixTask      Prefix = "tsk"
	PrefixMessage   Prefix = "msg"
	PrefixSession   Prefix = "ses"
	PrefixBatch     Prefix = "bat"
	PrefixJob       Prefix = "job"
	PrefixInput     Prefix = "in"
)

// idTimeWidth is the width of the base-36 timestamp in an ID. It is fixed
// so that IDs of one prefix sort by creation time as strings even for
// small timestamps, such as a test clock at the Unix epoch. 36^9 ms ends
// in the year 5138.
const idTimeWidth = 9

// NewID returns a new ID: the prefix, the time in base 36 milliseconds
// padded to a fixed width, and 8 random bytes.
func NewID(p Prefix) ID { return NewIDAt(p, time.Now()) }

// NewIDAt returns a new ID for ts. Tests inject a clock through it.
func NewIDAt(p Prefix, ts time.Time) ID {
	var b [8]byte
	_, _ = rand.Read(b[:])
	t := strconv.FormatInt(ts.UnixMilli(), 36)
	if n := idTimeWidth - len(t); n > 0 {
		t = strings.Repeat("0", n) + t
	}
	return ID(string(p) + "_" + t + hex.EncodeToString(b[:]))
}

// Prefix returns the type prefix of the ID, or "" when it has none.
func (id ID) Prefix() Prefix {
	p, _, ok := strings.Cut(string(id), "_")
	if !ok {
		return ""
	}
	return Prefix(p)
}
