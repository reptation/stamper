package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBundle(t *testing.T) {
	path := filepath.Join("..", "..", "policies", "dev.json")

	bundle, err := LoadBundle(path)
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}

	if bundle.Version != "v1" {
		t.Fatalf("expected version v1, got %q", bundle.Version)
	}
}

func TestValidateRejectsInvalidEffect(t *testing.T) {
	bundle := Bundle{
		Version: "v1",
		Policies: []Policy{
			{
				ID:        "POL-001",
				Name:      "invalid",
				Effect:    "block",
				Rationale: "nope",
			},
		},
	}

	err := bundle.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadBundleFailsForMissingFile(t *testing.T) {
	_, err := LoadBundle(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestLoadBundleFailsForInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("write invalid bundle: %v", err)
	}

	_, err := LoadBundle(path)
	if err == nil {
		t.Fatal("expected decode error")
	}
}
