package agentsession

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const Prefix = "asess_"

// CanonicalID returns the server-side grouping key for a client-provided agent
// session id. Already-canonical ids are returned unchanged so UI/API round trips
// do not double-hash.
func CanonicalID(orgID, client, actorID, rawSessionID string) string {
	rawSessionID = strings.TrimSpace(rawSessionID)
	if rawSessionID == "" {
		return ""
	}
	if IsCanonical(rawSessionID) {
		return rawSessionID
	}
	parts := []string{
		strings.TrimSpace(orgID),
		strings.TrimSpace(client),
		strings.TrimSpace(actorID),
		rawSessionID,
	}
	for i, part := range parts {
		if part == "" {
			parts[i] = "unknown"
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return Prefix + hex.EncodeToString(sum[:])[:32]
}

func IsCanonical(sessionID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionID), Prefix)
}
