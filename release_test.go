package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
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

// The save dialog on macOS adds the extension again unless the app exports the type of a backup file.
func TestBackupTypeIsExported(t *testing.T) {
	b, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MacOS struct {
			InfoPlist struct {
				Types []struct {
					Tags map[string][]string `json:"UTTypeTagSpecification"`
				} `json:"UTExportedTypeDeclarations"`
			} `json:"infoPlist"`
		} `json:"macos"`
	}
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	for _, d := range config.MacOS.InfoPlist.Types {
		if slices.Contains(d.Tags["public.filename-extension"], "oriel-backup") {
			return
		}
	}
	t.Error("mygo.json does not export a type for the oriel-backup extension")
}

// The deployment files are the ones docs/product.md shows, and the image builds with the Go version of the repository.
func TestDeploymentFiles(t *testing.T) {
	doc := readText(t, "docs/product.md")
	for _, file := range []string{"deploy/compose.yaml", "deploy/oriel.service"} {
		b := readText(t, file)
		if !strings.Contains(doc, "\n"+strings.TrimRight(b, "\n")+"\n```") {
			t.Errorf("docs/product.md does not show %s as it is", file)
		}
		if regexp.MustCompile(`(?i)password=[^<\s]`).MatchString(b) {
			t.Errorf("%s sets a password rather than a <placeholder>", file)
		}
	}
	dockerfile := readText(t, "Dockerfile")
	goVersion := readText(t, ".go-version")
	if want := "ARG GO_VERSION=" + strings.TrimSpace(goVersion) + "\n"; !strings.Contains(dockerfile, want) {
		t.Errorf("Dockerfile does not have %q", want)
	}
}

// readText reads a checked-in text file with its line endings as committed, since a Windows checkout turns them into CRLF.
func readText(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}
