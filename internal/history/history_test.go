package history

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRecordPutsTheLatestFirstWithoutDuplicates(t *testing.T) {
	s := Open("", 10)
	for _, id := range []string{"a", "b", "a", "c"} {
		s.Record(id)
	}
	if got, want := s.Recent(), []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Errorf("Recent() = %v, want %v", got, want)
	}
}

func TestRecordDropsTheOldestPastTheLimit(t *testing.T) {
	s := Open("", 2)
	for _, id := range []string{"a", "b", "c"} {
		s.Record(id)
	}
	if got, want := s.Recent(), []string{"c", "b"}; !slices.Equal(got, want) {
		t.Errorf("Recent() = %v, want %v", got, want)
	}
}

func TestHistorySurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.json")
	s := Open(path, 10)
	if err := s.Record("nav:prs"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	s.Record("act:refresh")

	if got, want := Open(path, 10).Recent(), []string{"act:refresh", "nav:prs"}; !slices.Equal(got, want) {
		t.Errorf("reopened Recent() = %v, want %v", got, want)
	}
}

func TestOpenTreatsAMissingOrCorruptFileAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := Open(filepath.Join(dir, "none.json"), 10).Recent(); len(got) != 0 {
		t.Errorf("missing file: Recent() = %v", got)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	s := Open(bad, 10)
	if got := s.Recent(); len(got) != 0 {
		t.Errorf("corrupt file: Recent() = %v", got)
	}
	// A corrupt file is replaced on the next write rather than wedging history.
	if err := s.Record("x"); err != nil {
		t.Fatalf("Record over a corrupt file: %v", err)
	}
	if got := Open(bad, 10).Recent(); !slices.Equal(got, []string{"x"}) {
		t.Errorf("after rewrite Recent() = %v", got)
	}
}

func TestOpenTrimsAFileLongerThanTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	os.WriteFile(path, []byte(`["a","b","c"]`), 0o600)
	if got := Open(path, 2).Recent(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Recent() = %v", got)
	}
}
