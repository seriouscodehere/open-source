package helper

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// SecureCompare performs constant-time comparison to prevent timing attacks
func SecureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// REMOVED: VerifyProofOfWork - challenge system no longer supported

// BlockStatusCookie tracks block status in client cookie
type BlockStatusCookie struct {
	IP           string    `json:"ip"`
	BlockedUntil time.Time `json:"blocked_until"`
	Reason       string    `json:"reason"`
}

// EncodeBlockStatus encodes block status to base64 string
func EncodeBlockStatus(status BlockStatusCookie) string {
	data, _ := json.Marshal(status)
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeBlockStatus decodes base64 string to block status
func DecodeBlockStatus(encoded string) (BlockStatusCookie, error) {
	var status BlockStatusCookie
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return status, err
	}
	err = json.Unmarshal(data, &status)
	return status, err
}
