package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

func generatePKCE() (verifier, challenge string) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

func generateState() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(raw)
}
