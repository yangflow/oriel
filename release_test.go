package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestUpdateConfig pins where installed copies look for updates and what they trust: only the public releases of
// yangflow/oriel, over HTTPS, signed with the key whose public half is in mygo.json. The secret half, the signing
// identity and the notary profile stay out of the repository (docs/release.md).
func TestUpdateConfig(t *testing.T) {
	b, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Updates map[string]any `json:"updates"`
		MacOS   map[string]any `json:"macos"`
	}
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range config.Updates {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"github", "publicKey"}) {
		t.Errorf("updates has %v, want only github and publicKey", keys)
	}
	if config.Updates["github"] != "yangflow/oriel" {
		t.Errorf("updates.github = %v, want yangflow/oriel", config.Updates["github"])
	}
	text, _ := config.Updates["publicKey"].(string)
	if key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text)); err != nil || len(key) != ed25519.PublicKeySize {
		t.Errorf("updates.publicKey is not an Ed25519 public key: %d bytes, %v", len(key), err)
	}
	for _, k := range []string{"signingIdentity", "notarize"} {
		if _, ok := config.MacOS[k]; ok {
			t.Errorf("macos.%s is in mygo.json; it belongs to the release workflow", k)
		}
	}
}
