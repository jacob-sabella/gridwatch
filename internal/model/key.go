package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// MatchKey generates a stable identifier for a match given its defining
// attributes. The same source+game+time+teams+tournament must always yield
// the same key, so dedupe state survives polling cycles.
//
// Input order matters; teams are sorted internally to make the key insensitive
// to home/away ordering when the upstream source shuffles them.
func MatchKey(source, game string, startUnix int64, team1, team2, tournament string) string {
	// Normalize team order for key stability. Upstream HTML sometimes swaps
	// left/right between polls; we don't want that to create a new match.
	t1, t2 := team1, team2
	if t2 < t1 {
		t1, t2 = t2, t1
	}
	h := sha256.New()
	h.Write([]byte(source))
	h.Write([]byte{'|'})
	h.Write([]byte(game))
	h.Write([]byte{'|'})
	h.Write([]byte(strconv.FormatInt(startUnix, 10)))
	h.Write([]byte{'|'})
	h.Write([]byte(strings.ToLower(t1)))
	h.Write([]byte{'|'})
	h.Write([]byte(strings.ToLower(t2)))
	h.Write([]byte{'|'})
	h.Write([]byte(tournament))
	sum := h.Sum(nil)
	return "m_" + hex.EncodeToString(sum[:6])
}
