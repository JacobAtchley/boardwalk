package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsTheGroups(t *testing.T) {
	t.Setenv(EnvPath, writeConfig(t, `{"reviewGroups": ["platform-devs", "Platform Leads"]}`))

	c, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if len(c.ReviewGroups) != 2 || c.ReviewGroups[0] != "platform-devs" {
		t.Errorf("groups = %v", c.ReviewGroups)
	}
}

func TestLoadWithNoConfigFileIsNotAnError(t *testing.T) {
	// boardwalk needs no config to run; the file only adds to what it knows.
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "absent.json"))

	c, err := Load()
	if err != nil {
		t.Fatalf("Load with no file returned %v", err)
	}
	if len(c.ReviewGroups) != 0 {
		t.Errorf("groups = %v, want none", c.ReviewGroups)
	}
}

func TestLoadReportsAMalformedConfig(t *testing.T) {
	// Silently ignoring a typo would leave the review filter quietly missing
	// pull requests, which is the failure this config exists to prevent.
	t.Setenv(EnvPath, writeConfig(t, `{"reviewGroups": "not-a-list"}`))

	if _, err := Load(); err == nil {
		t.Error("a malformed config loaded without complaint")
	}
}
