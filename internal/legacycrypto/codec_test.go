package legacycrypto

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type vector struct {
	Name, Secret, Plaintext, IV, Envelope string
	Domain                                Domain
}

func vectors(t *testing.T) []vector {
	t.Helper()
	data, err := os.ReadFile("testdata/compatibility.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Synthetic bool
		Vectors   []vector
	}
	if err = json.Unmarshal(data, &fixture); err != nil || !fixture.Synthetic || len(fixture.Vectors) != 15 {
		t.Fatal("required synthetic crypto vectors missing")
	}
	return fixture.Vectors
}
func TestLegacyCipherCompatibility(t *testing.T) {
	for _, v := range vectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			c, err := New(v.Secret, v.Domain)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := c.Decrypt(v.Envelope)
			if err != nil || plain != v.Plaintext {
				t.Fatal("legacy JS envelope not decrypted exactly")
			}
			iv, err := hex.DecodeString(v.IV)
			if err != nil {
				t.Fatal(err)
			}
			if c.seal(iv, v.Plaintext) != v.Envelope {
				t.Fatal("Go ciphertext/tag differ from JS")
			}
		})
	}
}
func TestLegacyCipherRefusesTampering(t *testing.T) {
	v := vectors(t)[0]
	c, _ := New(v.Secret, v.Domain)
	parts := strings.Split(v.Envelope, ".")
	bad := []string{"", "CI invalid envelope", parts[0], v.Envelope + ".extra", parts[0] + "." + parts[1] + "=", "!." + parts[1], parts[0] + ".AA", parts[0] + ".!!!!"}
	for _, part := range []int{0, 1} {
		data, _ := base64.RawURLEncoding.DecodeString(parts[part])
		data[len(data)-1] ^= 1
		changed := append([]string(nil), parts...)
		changed[part] = base64.RawURLEncoding.EncodeToString(data)
		bad = append(bad, strings.Join(changed, "."))
	}
	ciphertext, _ := base64.RawURLEncoding.DecodeString(parts[1])
	ciphertext[0] ^= 1
	bad = append(bad, parts[0]+"."+base64.RawURLEncoding.EncodeToString(ciphertext))

	for i, envelope := range bad {
		if plain, err := c.Decrypt(envelope); plain != "" || !errors.Is(err, ErrEnvelope) {
			t.Errorf("corrupted case %d returned data or wrong error", i)
		}
	}
	for _, domain := range []Domain{OpenAI, OpenRouter, Firecrawl, Apify} {
		other, _ := New(v.Secret, domain)
		if plain, err := other.Decrypt(v.Envelope); plain != "" || err != ErrEnvelope {
			t.Fatal("provider domain not isolated")
		}
	}
	wrong, _ := New("CI SYNTHETIC different secret", Calendar)
	if plain, err := wrong.Decrypt(v.Envelope); plain != "" || err != ErrEnvelope {
		t.Fatal("wrong secret returned plaintext")
	}
}
func TestLegacyCipherConfigurationAndFreshNonces(t *testing.T) {
	for _, v := range []struct {
		secret string
		domain Domain
	}{{"", Calendar}, {"CI SYNTHETIC secret", Domain(":invented")}, {string([]byte{255}), Calendar}} {
		if c, err := New(v.secret, v.domain); c != nil || err != ErrConfiguration {
			t.Fatal("invalid config accepted")
		}
	}
	var missing *Codec
	if _, err := missing.Decrypt(""); err != ErrConfiguration {
		t.Fatal("nil codec did not fail closed")
	}
	c, _ := New("CI SYNTHETIC entropy test", Apify)
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		encrypted, err := c.Encrypt("CI SYNTHETIC VALUE")
		if err != nil {
			t.Fatal(err)
		}
		iv := strings.Split(encrypted, ".")[0]
		if seen[iv] {
			t.Fatal("nonce repeated")
		}
		seen[iv] = true
		if plain, err := c.Decrypt(encrypted); err != nil || plain != "CI SYNTHETIC VALUE" {
			t.Fatal("fresh nonce round trip failed")
		}
	}
	for _, value := range []string{string([]byte{255}), strings.Repeat("x", maxPlaintextBytes+1)} {
		if _, err := c.Encrypt(value); err != ErrValue {
			t.Fatal("invalid value accepted")
		}
	}
	if plain, err := c.Decrypt(strings.Repeat("x", maxEnvelopeBytes+1)); plain != "" || err != ErrEnvelope {
		t.Fatal("oversized envelope accepted")
	}
}
