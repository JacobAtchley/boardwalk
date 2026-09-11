package main

import "testing"

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		start string
		rest  []string
	}{
		{"no arguments opens the menu", nil, "", nil},
		{"a subcommand jumps straight in", []string{"prs"}, "prs", nil},
		{"items is a subcommand too", []string{"items"}, "items", nil},
		{"builds is a subcommand", []string{"builds"}, "builds", nil},
		{"a flag is not a subcommand", []string{"-mine"}, "", []string{"-mine"}},
		{"flags after a subcommand survive", []string{"items", "-mine"}, "items", []string{"-mine"}},
		{"an unknown word is left to the flag parser", []string{"nonsense"}, "", []string{"nonsense"}},
	} {
		start, rest := parseArgs(tc.args)
		if start != tc.start {
			t.Errorf("%s: start = %q, want %q", tc.name, start, tc.start)
		}
		if len(rest) != len(tc.rest) {
			t.Errorf("%s: rest = %v, want %v", tc.name, rest, tc.rest)
		}
	}
}

func TestStartForFlags(t *testing.T) {
	// -mine and -all only make sense against work items, so they imply it.
	if got := startFor("", true, false); got != "items" {
		t.Errorf("startFor with -mine = %q, want items", got)
	}
	if got := startFor("", false, true); got != "items" {
		t.Errorf("startFor with -all = %q, want items", got)
	}
	if got := startFor("prs", true, false); got != "prs" {
		t.Errorf("an explicit subcommand = %q, want it respected", got)
	}
	if got := startFor("", false, false); got != "" {
		t.Errorf("startFor with no flags = %q, want the menu", got)
	}
}

// A word that survives flag parsing unclaimed used to vanish silently — a
// subcommand typed after a flag (the flag package stops at the first
// non-flag word) or a misspelled subcommand both left fs.Args() non-empty
// with nothing reading it. parseCommandLine must turn that into an error
// instead of quietly opening the wrong view, or the menu, with no feedback.
func TestParseCommandLineRejectsALeftoverArgument(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"a subcommand after a flag is discarded, not read", []string{"-mine", "prs"}},
		{"the same for -all", []string{"-all", "builds"}},
		{"a misspelled subcommand is left over too", []string{"builter"}},
	} {
		if _, err := parseCommandLine(tc.args); err == nil {
			t.Errorf("%s: parseCommandLine(%v) = nil error, want one naming the leftover argument", tc.name, tc.args)
		}
	}
}

func TestParseCommandLineAcceptsTheDocumentedOrderings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		start string
	}{
		{"a bare subcommand", []string{"prs"}, "prs"},
		{"a subcommand followed by a flag", []string{"items", "-mine"}, "items"},
		{"a flag alone implies items", []string{"-mine"}, "items"},
		{"no arguments opens the menu", nil, ""},
		{"-dump alone does not force a view", []string{"-dump"}, ""},
		{"-version alone does not force a view", []string{"-version"}, ""},
	} {
		f, err := parseCommandLine(tc.args)
		if err != nil {
			t.Errorf("%s: parseCommandLine(%v) = error %v, want none", tc.name, tc.args, err)
			continue
		}
		if f.start != tc.start {
			t.Errorf("%s: start = %q, want %q", tc.name, f.start, tc.start)
		}
	}
}
