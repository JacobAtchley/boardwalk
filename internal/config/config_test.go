package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boardwalk.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsEverySetting(t *testing.T) {
	t.Setenv(EnvPath, writeConfig(t, `{
		"org": "acme",
		"project": "Platform",
		"reviewGroups": ["platform-devs", "Platform Leads"],
		"paletteKey": "ctrl+k"
	}`))

	c, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if c.Org != "acme" || c.Project != "Platform" {
		t.Errorf("org/project = %q/%q", c.Org, c.Project)
	}
	if len(c.ReviewGroups) != 2 || c.ReviewGroups[0] != "platform-devs" {
		t.Errorf("groups = %v", c.ReviewGroups)
	}
	if c.PaletteKey != "ctrl+k" {
		t.Errorf("paletteKey = %q", c.PaletteKey)
	}
}

func TestHistoryPathSitsBesideTheConfig(t *testing.T) {
	// Each config keeps its own history, so a second organisation's recent
	// actions do not crowd out the first's.
	for cfg, want := range map[string]string{
		"/home/me/.config/boardwalk.json":       "/home/me/.config/boardwalk.history.json",
		"/home/me/.config/boardwalk.other.json": "/home/me/.config/boardwalk.other.history.json",
		"/etc/boardwalk":                        "/etc/boardwalk.history.json",
	} {
		t.Setenv(EnvPath, cfg)
		if got, err := HistoryPath(); err != nil || got != want {
			t.Errorf("HistoryPath() for %s = %q, %v; want %q", cfg, got, err, want)
		}
	}
}

func TestLoadDistinguishesAMissingConfigFromABrokenOne(t *testing.T) {
	// "You have not set boardwalk up" and "your config is wrong" want
	// different messages, so the caller has to be able to tell them apart.
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "absent.json"))

	_, err := Load()
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("Load with no file returned %v, want ErrMissing", err)
	}
	if !strings.Contains(err.Error(), "absent.json") {
		t.Errorf("error = %q, want it to name the path it looked at", err)
	}
}

func TestLoadReportsAMalformedConfig(t *testing.T) {
	t.Setenv(EnvPath, writeConfig(t, `{"org": "acme",}`))

	_, err := Load()
	if err == nil {
		t.Fatal("a malformed config loaded without complaint")
	}
	if errors.Is(err, ErrMissing) {
		t.Error("a malformed config was reported as a missing one")
	}
}

func TestPathDefaultsToDotConfig(t *testing.T) {
	// Not os.UserConfigDir: that answers ~/Library/Application Support on
	// macOS, which is not where anyone keeping dotfiles would look.
	t.Setenv(EnvPath, "")
	t.Setenv("XDG_CONFIG_HOME", "")

	path, err := Path()
	if err != nil {
		t.Fatalf("Path returned %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	if want := filepath.Join(home, ".config", "boardwalk.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestPathHonoursXDGConfigHome(t *testing.T) {
	t.Setenv(EnvPath, "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	path, err := Path()
	if err != nil {
		t.Fatalf("Path returned %v", err)
	}
	// Joined rather than spelled out: Path builds with filepath.Join, so the
	// separator is the platform's, and a literal "/xdg/boardwalk.json" here
	// only ever described the Unix half of that.
	if want := filepath.Join("/xdg", "boardwalk.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestPathHonoursTheOverride(t *testing.T) {
	t.Setenv(EnvPath, "/tmp/elsewhere.json")

	path, err := Path()
	if err != nil {
		t.Fatalf("Path returned %v", err)
	}
	if path != "/tmp/elsewhere.json" {
		t.Errorf("path = %q, want the override", path)
	}
}

func TestValidateNamesWhatIsMissingAndWhere(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    Config
		want string
	}{
		{"neither", Config{}, "neither org nor project"},
		{"no org", Config{Project: "Platform"}, "does not set org"},
		{"no project", Config{Org: "acme"}, "does not set project"},
	} {
		err := tc.c.Validate("/home/dev/.config/boardwalk.json")
		if err == nil {
			t.Errorf("%s: Validate returned no error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %q, want it to mention %q", tc.name, err, tc.want)
		}
		if !strings.Contains(err.Error(), "boardwalk.json") {
			t.Errorf("%s: error = %q, want it to name the file", tc.name, err)
		}
	}
}

func TestValidateAcceptsAConfigWithBothSet(t *testing.T) {
	c := Config{Org: "acme", Project: "Platform"}

	if err := c.Validate("/anywhere"); err != nil {
		t.Errorf("Validate returned %v for a complete config", err)
	}
}

func TestExampleIsItselfValid(t *testing.T) {
	// The example is what an error message tells someone to copy, so it had
	// better load.
	path := writeConfig(t, Example())
	t.Setenv(EnvPath, path)

	c, err := Load()
	if err != nil {
		t.Fatalf("the example config does not parse: %v", err)
	}
	if err := c.Validate(path); err != nil {
		t.Errorf("the example config does not validate: %v", err)
	}
}
