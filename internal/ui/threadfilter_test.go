package ui

import (
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
)

func TestThreadFilterCyclesThroughAllThreeModes(t *testing.T) {
	f := filterAll
	for _, want := range []threadFilter{filterUnresolved, filterResolved, filterAll} {
		if f = f.next(); f != want {
			t.Fatalf("next = %v, want %v", f, want)
		}
	}
}

func TestThreadFilterKeeps(t *testing.T) {
	resolved := azdo.Thread{Status: "fixed", Resolved: true}
	active := azdo.Thread{Status: "active"}
	// Azure DevOps leaves a status unset on some threads. Summarize counts
	// those in neither tally, but they are not settled either, so the
	// unresolved filter has to show them — hiding them would lose discussion
	// with nothing on screen to say it is missing.
	unset := azdo.Thread{}

	for _, tc := range []struct {
		name                                string
		f                                   threadFilter
		wantResolved, wantActive, wantUnset bool
	}{
		{"all keeps everything", filterAll, true, true, true},
		{"unresolved drops the settled", filterUnresolved, false, true, true},
		{"resolved keeps only the settled", filterResolved, true, false, false},
	} {
		if got := tc.f.keep(resolved); got != tc.wantResolved {
			t.Errorf("%s: keep(resolved) = %v, want %v", tc.name, got, tc.wantResolved)
		}
		if got := tc.f.keep(active); got != tc.wantActive {
			t.Errorf("%s: keep(active) = %v, want %v", tc.name, got, tc.wantActive)
		}
		if got := tc.f.keep(unset); got != tc.wantUnset {
			t.Errorf("%s: keep(status unset) = %v, want %v", tc.name, got, tc.wantUnset)
		}
	}
}

func TestThreadFilterKeepsInOrder(t *testing.T) {
	threads := []azdo.Thread{
		{ID: 1, Status: "fixed", Resolved: true},
		{ID: 2, Status: "active"},
		{ID: 3, Status: "closed", Resolved: true},
	}

	kept := filterResolved.apply(threads)

	if len(kept) != 2 || kept[0].ID != 1 || kept[1].ID != 3 {
		t.Errorf("apply = %v, want threads 1 and 3 in that order", kept)
	}
	if len(threads) != 3 {
		t.Error("apply rewrote the slice it was given")
	}
}

func TestThreadFilterNames(t *testing.T) {
	for _, tc := range []struct {
		f          threadFilter
		noun, want string
	}{
		{filterAll, "", "all"},
		{filterUnresolved, "unresolved", "unresolved only"},
		{filterResolved, "resolved", "resolved only"},
	} {
		if got := tc.f.noun(); got != tc.noun {
			t.Errorf("noun = %q, want %q", got, tc.noun)
		}
		if got := tc.f.label(); got != tc.want {
			t.Errorf("label = %q, want %q", got, tc.want)
		}
	}
}
