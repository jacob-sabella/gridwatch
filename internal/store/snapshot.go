package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jsabella/gridwatch/internal/model"
)

// SaveSnapshot writes the current store contents to path atomically
// (write tmp, fsync, rename).
func (s *Store) SaveSnapshot(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	matches := s.All()
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshotFile{Matches: matches}); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return os.Rename(tmp, path)
}

// LoadSnapshot reads a previously-saved snapshot into the store. Missing
// file is not an error (first boot). Corrupt file returns an error so the
// operator can decide whether to delete it.
func (s *Store) LoadSnapshot(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	var sf snapshotFile
	if err := json.NewDecoder(f).Decode(&sf); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	s.Load(sf.Matches)
	return nil
}

// snapshotFile is the on-disk shape. Versioned so we can evolve without
// breaking upgrades.
type snapshotFile struct {
	Version int           `json:"version"`
	Matches []model.Match `json:"matches"`
}
