//go:build legacyinterop

package backup

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyV39Contract(t *testing.T) {
	cmd := exec.Command("node", filepath.Join("..", "..", "scripts", "check-v39-contract.mjs"), "--stdio")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal("cannot start required source oracle")
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder := json.NewDecoder(bufio.NewReader(pipe))
	checked := 0
	for {
		var v struct {
			Type, Name, Body, Digest, BodySpec, Normalized, FormattedHash string
			OK                                                            bool
			Length                                                        int64
			Status, EncodedStatus                                         int
		}
		err = decoder.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("source oracle output invalid")
		}
		checked++
		t.Run(v.Name, func(t *testing.T) {
			if v.Type == "request" {
				body := v.BodySpec
				if body == "oversize-spaces" {
					body = strings.Repeat(" ", MaxBytes+1)
				}
				if body == "exact-limit-spaces" {
					body = "{}" + strings.Repeat(" ", MaxBytes-2)
				}
				if strings.HasPrefix(body, "hex:") {
					decoded, e := hex.DecodeString(strings.TrimPrefix(body, "hex:"))
					if e != nil {
						t.Fatal(e)
					}
					body = string(decoded)
				}
				normalized, err := ReadRequest(strings.NewReader(body), v.Length)
				if ErrorStatus(err) != v.Status {
					t.Fatalf("request status differs: %d", ErrorStatus(err))
				}
				if v.Normalized != "" {
					n, e := orderedJSON(normalized)
					if e != nil || string(n.stringify(nil, false)) != v.Normalized {
						t.Fatal("TextDecoder UTF8 replacement differs")
					}
				}
				return
			}
			got, err := DigestJSON([]byte(v.Body))
			if err != nil || got != v.Digest {
				t.Fatal("SHA-256 differs from unchanged JSON.stringify digest")
			}
			if v.Type == "validation" {
				_, err := ValidateV39([]byte(v.Body))
				if (err == nil) != v.OK {
					t.Fatalf("legacy layout decision differs: %v", err)
				}
				if v.Name == "overflow numeric cell before stringify" {
					if output, e := EncodeV39([]byte(v.Body)); ErrorStatus(e) != 400 || len(output) != 0 {
						t.Fatal("export silently replaced a non-finite SQL scalar")
					}
				}
				if v.OK {
					formatted, e := EncodeV39([]byte(v.Body))
					if ErrorStatus(e) != v.EncodedStatus {
						t.Fatalf("formatted export status differs: %d", ErrorStatus(e))
					}
					if v.EncodedStatus != 200 {
						if len(formatted) != 0 {
							t.Fatal("oversized export returned partial bytes")
						}
						return
					}
					if e != nil {
						t.Fatalf("valid export formatting failed: %v", e)
					}
					hash := sha256.Sum256(formatted)
					if hex.EncodeToString(hash[:]) != v.FormattedHash {
						t.Fatal("formatted export differs from JSON.stringify(payload,null,2)")
					}
				}
			}
		})
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("required source oracle failed: %s", stderr.String())
	}
	if checked != 67 {
		t.Fatalf("required source cases missing: %d", checked)
	}
}
