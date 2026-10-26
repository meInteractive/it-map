// Package legacycrypto implements the canonical envelope emitted by the Sites
// integration helpers. It does not read environment variables or stored keys.
package legacycrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"
)

type Domain string

const (
	Calendar   Domain = ""
	OpenAI     Domain = ":openai:v1"
	OpenRouter Domain = ":openrouter:v1" // JEV uses the OpenRouter key.
	Firecrawl  Domain = ":firecrawl:v1"  // Both free and hobby use this domain.
	Apify      Domain = ":apify:v1"      // Both free and starter use this domain.
)

var (
	ErrConfiguration = errors.New("legacy crypto configuration unavailable")
	ErrEnvelope      = errors.New("legacy crypto envelope invalid")
	ErrValue         = errors.New("legacy crypto value invalid")
)

const maxPlaintextBytes = 64 * 1024
const maxEnvelopeBytes = 100 * 1024

// Codec is immutable after construction and may be shared by goroutines.
// Treat it as secret-bearing state: never log or serialize a Codec.
type Codec struct{ aead cipher.AEAD }

func New(secret string, domain Domain) (*Codec, error) {
	switch domain {
	case Calendar, OpenAI, OpenRouter, Firecrawl, Apify:
	default:
		return nil, ErrConfiguration
	}
	if secret == "" || !utf8.ValidString(secret) {
		return nil, ErrConfiguration
	}
	digest := sha256.Sum256([]byte("google-calendar:" + secret + string(domain)))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, ErrConfiguration
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrConfiguration
	}
	return &Codec{aead: aead}, nil
}

func (c *Codec) Encrypt(value string) (string, error) {
	if c == nil || c.aead == nil {
		return "", ErrConfiguration
	}
	if !utf8.ValidString(value) || len(value) > maxPlaintextBytes {
		return "", ErrValue
	}
	iv := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", ErrConfiguration
	}
	return c.seal(iv, value), nil
}

// Only tests use a deterministic nonce; Encrypt always obtains fresh entropy.
func (c *Codec) seal(iv []byte, value string) string {
	sealed := c.aead.Seal(nil, iv, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(iv) + "." + base64.RawURLEncoding.EncodeToString(sealed)
}

func (c *Codec) Decrypt(envelope string) (string, error) {
	if c == nil || c.aead == nil {
		return "", ErrConfiguration
	}
	if len(envelope) > maxEnvelopeBytes {
		return "", ErrEnvelope
	}
	parts := strings.Split(envelope, ".")
	if len(parts) != 2 {
		return "", ErrEnvelope
	}
	encoding := base64.RawURLEncoding.Strict()
	iv, err := encoding.DecodeString(parts[0])
	if err != nil || len(iv) != c.aead.NonceSize() {
		return "", ErrEnvelope
	}
	sealed, err := encoding.DecodeString(parts[1])
	if err != nil || len(sealed) < c.aead.Overhead() || len(sealed) > maxPlaintextBytes+c.aead.Overhead() {
		return "", ErrEnvelope
	}
	// JS emits unpadded URL-safe encoding. Reject alternate/trailing encodings
	// rather than accepting malformed records as a different valid envelope.
	if encoding.EncodeToString(iv) != parts[0] || encoding.EncodeToString(sealed) != parts[1] {
		return "", ErrEnvelope
	}
	plain, err := c.aead.Open(nil, iv, sealed, nil)
	if err != nil || !utf8.Valid(plain) {
		return "", ErrEnvelope
	}
	return string(plain), nil
}
