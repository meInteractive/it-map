//go:build legacyinterop

package legacycrypto

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

// Explicit CI target; missing Node is an error, never a successful skip.
func TestGoEnvelopesDecryptInLegacyJS(t *testing.T) {
	type row struct {
		Secret    string `json:"secret"`
		Domain    Domain `json:"domain"`
		Plaintext string `json:"plaintext"`
		Envelope  string `json:"envelope"`
	}
	rows := []row{}
	for _, v := range vectors(t) {
		c, err := New(v.Secret, v.Domain)
		if err != nil {
			t.Fatal(err)
		}
		encrypted, err := c.Encrypt(v.Plaintext)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row{v.Secret, v.Domain, v.Plaintext, encrypted})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "../../scripts/check-legacy-crypto.mjs", "--verify-go")
	command.Stdin = bytes.NewReader(data)
	if err = command.Run(); err != nil {
		t.Fatal("required Go to legacy JS decryption failed")
	}
}
