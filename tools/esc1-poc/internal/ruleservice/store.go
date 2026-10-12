/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package ruleservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
)

var ErrNoActive = errors.New("no active rule bundle")

// Status records only the latest worker report, rather than growing a history
// containing query text, credentials, or collected graph data.
type Status struct {
	State     string    `json:"state"`
	Event     string    `json:"event"`
	Revision  string    `json:"revision"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct {
	mutex     sync.Mutex
	directory string
	active    Bundle
	plan      extension.Plan
	status    Status
}

// Open restores a validated active snapshot. Corrupt state is an error; it never
// silently becomes an empty bundle that a worker could mistake for a deletion.
func Open(directory string) (*Store, error) {
	var store = &Store{directory: directory, status: Status{State: "no-rules", Event: "not-reported", Message: "No execution report received."}}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	entry, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("rule storage must be a directory, not a symbolic link")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	contents, err := readPrivate(filepath.Join(directory, "active.json"), MaxBundleBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := decodeJSON(contents, &store.active); err != nil {
			return nil, fmt.Errorf("read active rules: %w", err)
		}
		store.plan, err = validateBundle(directory, store.active)
		if err != nil {
			return nil, fmt.Errorf("validate stored rules: %w", err)
		}
	}
	contents, err = readPrivate(filepath.Join(directory, "status.json"), MaxStatusBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := decodeJSON(contents, &store.status); err != nil {
			return nil, err
		}
		if err := validateStatus(store.status); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) Active() (Bundle, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.active.Revision == "" {
		return Bundle{}, ErrNoActive
	}
	return cloneBundle(s.active), nil
}

func cloneBundle(bundle Bundle) Bundle {
	copied := Bundle{Revision: bundle.Revision, Files: make(map[string]string, len(bundle.Files)), RetiredRelationships: append([]string(nil), bundle.RetiredRelationships...), RetiredEndpoints: copyEndpoints(bundle.RetiredEndpoints), Disabled: append([]string(nil), bundle.Disabled...), AddedAt: map[string]time.Time{}}
	for name, added := range bundle.AddedAt {
		copied.AddedAt[name] = added
	}
	for name, contents := range bundle.Files {
		copied.Files[name] = contents
	}
	return copied
}

// Update is for trusted, in-process setup (including test fixtures). Browser
// and API changes must use Preview and Apply so replacements are reviewed.
func (s *Store) Update(files map[string]string) (Bundle, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var change = Change{BaseRevision: s.active.Revision, Files: files}
	for name := range files {
		change.Overwrite = append(change.Overwrite, name)
	}
	candidate, plan, _, err := s.candidate(change)
	if err != nil {
		return Bundle{}, err
	}
	return s.activate(candidate, plan)
}

func (s *Store) activate(candidate Bundle, plan extension.Plan) (Bundle, error) {
	if candidate.Revision == s.active.Revision {
		return cloneBundle(s.active), nil
	}
	// Keep the first recorded date through edits and disabling. Older saved
	// rules without a recorded date stay unknown instead of receiving a guess.
	for name := range candidate.Files {
		if !queryName.MatchString(name) {
			continue
		}
		if _, existed := s.active.Files[name]; !existed {
			candidate.AddedAt[name] = time.Now().UTC()
		}
	}
	for name := range candidate.AddedAt {
		if _, exists := candidate.Files[name]; !exists {
			delete(candidate.AddedAt, name)
		}
	}
	if err := checkRetirements(candidate, plan); err != nil {
		return Bundle{}, err
	}
	contents, err := json.Marshal(candidate)
	if err != nil {
		return Bundle{}, err
	}
	if err := writePrivate(s.directory, "active.json", contents); err != nil {
		return Bundle{}, err
	}
	s.active = candidate
	s.plan = plan
	return cloneBundle(s.active), nil
}

func (s *Store) LatestStatus() Status {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.status
}

func (s *Store) Report(status Status) error {
	if err := validateStatus(status); err != nil {
		return err
	}
	status.UpdatedAt = time.Now().UTC()
	contents, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if len(contents) > MaxStatusBytes {
		return fmt.Errorf("status exceeds its byte limit")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := writePrivate(s.directory, "status.json", contents); err != nil {
		return err
	}
	s.status = status
	return nil
}

func validateStatus(status Status) error {
	if status.State != "success" && status.State != "failed" && status.State != "running" && status.State != "no-rules" {
		return fmt.Errorf("status state must be success, failed, running, or no-rules")
	}
	if len(status.Event) > 128 || len(status.Message) > 2048 || !utf8.ValidString(status.Event) || !utf8.ValidString(status.Message) {
		return fmt.Errorf("status event or message exceeds its text limit")
	}
	if status.Revision != "" {
		if len(status.Revision) != 64 {
			return fmt.Errorf("status revision must be a SHA-256 digest")
		}
		for _, letter := range status.Revision {
			if !(letter >= '0' && letter <= '9' || letter >= 'a' && letter <= 'f') {
				return fmt.Errorf("status revision must be a SHA-256 digest")
			}
		}
	}
	return nil
}

func readPrivate(path string, limit int64) ([]byte, error) {
	entry, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !entry.Mode().IsRegular() || entry.Size() > limit {
		return nil, fmt.Errorf("stored state is not a bounded regular file")
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("stored state exceeds its byte limit")
	}
	return contents, nil
}

func writePrivate(directory, name string, contents []byte) error {
	file, err := os.CreateTemp(directory, "replace-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(contents); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, name))
}
