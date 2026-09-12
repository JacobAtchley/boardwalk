// Package config reads boardwalk's optional settings file.
//
// boardwalk runs without one: AZDO_ORG and AZDO_PROJECT are the whole of the
// required setup. The file carries what boardwalk cannot work out for itself.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// EnvPath overrides where the config is read from, which is what the tests use
// and what lets someone keep more than one.
const EnvPath = "BOARDWALK_CONFIG"

type Config struct {
	// ReviewGroups are the teams and security groups you belong to, named as
	// Azure DevOps displays them.
	//
	// A pull request can list a group as its reviewer rather than a person, and
	// nothing in the pull request payload says who is in that group — resolving
	// it means the Graph API, a different host, and a walk through nested
	// memberships. Naming them here is exact, costs no requests, and works
	// offline; the price is that it goes stale when your memberships change.
	ReviewGroups []string `json:"reviewGroups"`
}

// Load reads the config, treating a missing file as an empty one. A file that
// exists but cannot be parsed is an error: silently ignoring a typo would leave
// the review filter quietly missing pull requests, which is the thing the
// config is there to prevent.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("could not read %s: %w", path, err)
	}

	var c Config
	if err := json.Unmarshal(body, &c); err != nil {
		return Config{}, fmt.Errorf("could not parse %s: %w", path, err)
	}
	return c, nil
}

// Path is where the config lives.
func Path() (string, error) {
	if p := os.Getenv(EnvPath); p != "" {
		return p, nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not find a config directory: %w", err)
	}
	return filepath.Join(dir, "boardwalk", "config.json"), nil
}
