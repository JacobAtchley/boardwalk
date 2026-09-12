// Package config reads boardwalk's settings file.
//
// Everything boardwalk needs to know lives in one JSON file, so there is a
// single place to look and a single place to add to.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// EnvPath overrides where the config is read from, for keeping more than one —
// a second organisation, or a project you only visit occasionally.
const EnvPath = "BOARDWALK_CONFIG"

// Config is the whole of boardwalk's configuration.
type Config struct {
	// Org is the Azure DevOps organisation, the first path segment of
	// https://dev.azure.com/{org}.
	Org string `json:"org"`

	// Project is the team project within it.
	Project string `json:"project"`

	// ReviewGroups are the teams and security groups you belong to, named as
	// Azure DevOps displays them.
	//
	// A pull request can list a group as its reviewer rather than a person, and
	// nothing in the pull request payload says who is in that group — resolving
	// it means the Graph API, a different host, and a walk through nested
	// memberships. Naming them here is exact, costs no requests and works
	// offline; the price is that it goes stale when your memberships change.
	ReviewGroups []string `json:"reviewGroups"`
}

// ErrMissing is returned when there is no config file at all, so a caller can
// tell "you have not set boardwalk up yet" apart from "your config is wrong".
var ErrMissing = errors.New("no config file")

// Load reads the config.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("%w at %s", ErrMissing, path)
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

// Path is where the config lives: ~/.config/boardwalk.json, or wherever
// BOARDWALK_CONFIG or XDG_CONFIG_HOME says.
//
// The directory is spelled out rather than taken from os.UserConfigDir, which
// answers ~/Library/Application Support on macOS. ~/.config is where someone
// who keeps their dotfiles in one place will look, on every platform boardwalk
// runs on.
func Path() (string, error) {
	if p := os.Getenv(EnvPath); p != "" {
		return p, nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "boardwalk.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find a home directory: %w", err)
	}
	return filepath.Join(home, ".config", "boardwalk.json"), nil
}

// Validate reports what is missing, naming the file so the message says where
// to fix it rather than only what is wrong.
func (c Config) Validate(path string) error {
	switch {
	case c.Org == "" && c.Project == "":
		return fmt.Errorf("%s sets neither org nor project", path)
	case c.Org == "":
		return fmt.Errorf("%s does not set org", path)
	case c.Project == "":
		return fmt.Errorf("%s does not set project", path)
	}
	return nil
}

// Example is what a working config looks like, for an error message to show
// rather than describe.
func Example() string {
	c := Config{
		Org:          "my-org",
		Project:      "MyProject",
		ReviewGroups: []string{"platform-devs"},
	}
	body, _ := json.MarshalIndent(c, "", "  ")
	return string(body)
}
