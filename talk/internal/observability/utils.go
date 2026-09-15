package observability

import (
	"crypto/rand"
	"encoding/hex"
)

// generateSpanID generates a random 8-byte span ID as a hex string
func generateSpanID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
