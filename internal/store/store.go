// Package store keeps WSLPilot's own metadata: JSON documents with a
// schema version, written atomically under one data directory. Upstream
// objects (distributions, containers, volumes) are never stored here; see
// doc/en-us/architecture.md "Upstream owns state".
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Store is a directory of JSON documents.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// Default returns the store in %APPDATA%\WSLPilot (os.UserConfigDir),
// or in $WSLPILOT_DATA when set (tests and portable use).
func Default() (*Store, error) {
	if d := os.Getenv("WSLPILOT_DATA"); d != "" {
		return &Store{Dir: d}, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{Dir: filepath.Join(base, "WSLPilot")}, nil
}

// Path returns the absolute path of a document name such as
// "projects/abc/project.json".
func (s *Store) Path(name string) string { return filepath.Join(s.Dir, filepath.FromSlash(name)) }

// envelope is the on-disk shape of every document.
type envelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Data          json.RawMessage `json:"data"`
}

// ErrNewerSchema means the document was written by a newer WSLPilot.
var ErrNewerSchema = errors.New("store: document written by a newer WSLPilot")

// Load reads a document into v.
// Inputs: maxVersion is the newest schema this build understands.
// Returns: the stored schema version; fs.ErrNotExist when the document is
// missing; ErrNewerSchema (and v untouched) when it is newer than
// maxVersion, so an old build never rewrites a newer file.
func (s *Store) Load(name string, maxVersion int, v any) (int, error) {
	b, err := os.ReadFile(s.Path(name))
	if err != nil {
		return 0, err
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return 0, fmt.Errorf("store: %s: %w", name, err)
	}
	if env.SchemaVersion > maxVersion {
		return env.SchemaVersion, ErrNewerSchema
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		return env.SchemaVersion, fmt.Errorf("store: %s: %w", name, err)
	}
	return env.SchemaVersion, nil
}

// Save writes v as schema version.
// Side effects: creates parent directories; writes name.tmp, fsyncs and
// renames over name, so readers see either the old or the new document.
func (s *Store) Save(name string, version int, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(envelope{SchemaVersion: version, Data: data}, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.Path(name), append(b, '\n'))
}

// Delete removes a document or a directory of documents.
func (s *Store) Delete(name string) error {
	err := os.RemoveAll(s.Path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// List returns the entry names directly under dir (e.g. "projects").
func (s *Store) List(dir string) ([]string, error) {
	es, err := os.ReadDir(s.Path(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names, nil
}

// WriteFileAtomic writes data to path through a temporary file and a rename.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
