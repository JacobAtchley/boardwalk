// Package history remembers what the command palette ran, most recent first,
// so the actions someone reaches for most are the first ones offered.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// Store is a capped, most-recent-first list of ids, kept in a JSON file.
//
// It is not safe for concurrent use; the palette only touches it from the
// bubbletea update loop.
type Store struct {
	path  string
	limit int
	ids   []string
}

// Open reads the history at path. A missing or unreadable file is an empty
// history rather than an error: history is a convenience, and losing it must
// never stop boardwalk starting. An empty path keeps history in memory only.
func Open(path string, limit int) *Store {
	s := &Store{path: path, limit: max(1, limit)}
	if path == "" {
		return s
	}
	if body, err := os.ReadFile(path); err == nil {
		var ids []string
		if json.Unmarshal(body, &ids) == nil {
			s.ids = ids[:min(len(ids), s.limit)]
		}
	}
	return s
}

// Recent is every remembered id, most recent first. The slice is the store's
// own and must not be modified; handing it out uncopied saves an allocation
// on every palette open.
func (s *Store) Recent() []string { return s.ids }

// Record moves id to the front, dropping the oldest past the limit, and
// writes the file. The in-memory list is updated even when the write fails,
// so the session still sees it.
func (s *Store) Record(id string) error {
	if i := slices.Index(s.ids, id); i >= 0 {
		// Shift the ones before it down one, in place.
		copy(s.ids[1:i+1], s.ids[:i])
		s.ids[0] = id
	} else {
		if len(s.ids) < s.limit {
			s.ids = append(s.ids, "")
		}
		copy(s.ids[1:], s.ids)
		s.ids[0] = id
	}
	return s.save()
}

// save writes through a temporary file and a rename, so a crash mid-write
// leaves the old history rather than half of a new one.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	body, err := json.Marshal(s.ids)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
