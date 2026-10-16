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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxEditorWorkingCopies      = 128
	maxEditorWorkingCopyOwner   = 256
	maxEditorWorkingCopyName    = 256
	maxEditorWorkingCopyStorage = MaxBundleBytes
)

var ErrNoEditorWorkingCopy = errors.New("editor working copy does not exist")

// EditorWorkingCopy is unfinished editor state. It deliberately contains no
// parsed rule details: an author can save incomplete Cypher and declarations.
// The owner is accepted only from the native server and is never returned.
type EditorWorkingCopy struct {
	Name         string `json:"name"`
	Text         string `json:"text"`
	BaseRevision string `json:"base_revision"`
	Revision     string `json:"revision"`
}

// EditorWorkingCopySummary lets an editor show its saved files without loading
// every unfinished query into memory or returning another author's text.
type EditorWorkingCopySummary struct {
	Name         string `json:"name"`
	BaseRevision string `json:"base_revision"`
	Revision     string `json:"revision"`
}

type EditorWorkingCopyDeletion struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

// DecodeEditorWorkingCopy accepts only the fields an editor is allowed to
// choose. The native HTTP handler supplies the authenticated owner separately.
func DecodeEditorWorkingCopy(contents []byte) (EditorWorkingCopy, error) {
	var copy *EditorWorkingCopy
	if len(contents) > MaxBundleBytes {
		return EditorWorkingCopy{}, fmt.Errorf("working copy exceeds its byte limit")
	}
	if err := decodeJSON(contents, &copy); err != nil || copy == nil {
		return EditorWorkingCopy{}, fmt.Errorf("invalid working copy")
	}
	if err := validateEditorWorkingCopy(*copy); err != nil {
		return EditorWorkingCopy{}, fmt.Errorf("invalid working copy")
	}
	return *copy, nil
}

func DecodeEditorWorkingCopyDeletion(contents []byte) (EditorWorkingCopyDeletion, error) {
	var deletion *EditorWorkingCopyDeletion
	if len(contents) > MaxBundleBytes {
		return EditorWorkingCopyDeletion{}, fmt.Errorf("working copy exceeds its byte limit")
	}
	if err := decodeJSON(contents, &deletion); err != nil || deletion == nil || len(deletion.Name) == 0 || len(deletion.Name) > maxEditorWorkingCopyName || !queryName.MatchString(deletion.Name) || !validWorkingCopyRevision(deletion.Revision) || deletion.Revision == "" {
		return EditorWorkingCopyDeletion{}, fmt.Errorf("invalid working copy deletion")
	}
	return *deletion, nil
}

// storedEditorWorkingCopy keeps the owner beside the saved contents. The
// hashed filename is only an address; this check prevents a swapped private
// file from being read as a different owner's work.
type storedEditorWorkingCopy struct {
	Owner string `json:"owner"`
	EditorWorkingCopy
}

func editorWorkingCopyRevision(copy EditorWorkingCopy) string {
	copy.Revision = ""
	contents, _ := json.Marshal(copy)
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func validateEditorWorkingCopyOwner(owner string) error {
	if owner == "" || len(owner) > maxEditorWorkingCopyOwner || !utf8.ValidString(owner) {
		return fmt.Errorf("working copy owner is invalid")
	}
	return nil
}

func validateEditorWorkingCopy(copy EditorWorkingCopy) error {
	if len(copy.Name) == 0 || len(copy.Name) > maxEditorWorkingCopyName || !queryName.MatchString(copy.Name) {
		return fmt.Errorf("working copy name is invalid")
	}
	if len(copy.Text) > MaxFileBytes || !utf8.ValidString(copy.Text) {
		return fmt.Errorf("working copy text is invalid")
	}
	if !validWorkingCopyRevision(copy.BaseRevision) || !validWorkingCopyRevision(copy.Revision) {
		return fmt.Errorf("working copy revision is invalid")
	}
	return nil
}

func validWorkingCopyRevision(revision string) bool {
	if revision == "" {
		return true
	}
	if len(revision) != 64 {
		return false
	}
	for _, character := range revision {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) editorWorkingCopyDirectory() (string, error) {
	directory := filepath.Join(s.directory, "editor-working-copies")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	entry, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("editor working copy storage must be a directory")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}

// The hash keeps owner and filename out of paths. A caller can never choose a
// filesystem path, and the saved record still verifies both original values.
func editorWorkingCopyFilename(owner, name string) string {
	digest := sha256.Sum256([]byte(owner + "\x00" + name))
	return hex.EncodeToString(digest[:]) + ".json"
}

func (s *Store) readEditorWorkingCopy(directory, owner, name string) (EditorWorkingCopy, error) {
	contents, err := readPrivate(filepath.Join(directory, editorWorkingCopyFilename(owner, name)), maxEditorWorkingCopyStorage)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EditorWorkingCopy{}, ErrNoEditorWorkingCopy
		}
		return EditorWorkingCopy{}, err
	}
	var stored storedEditorWorkingCopy
	if err := decodeJSON(contents, &stored); err != nil {
		return EditorWorkingCopy{}, fmt.Errorf("read editor working copy: %w", err)
	}
	if stored.Owner != owner || stored.Name != name || validateEditorWorkingCopyOwner(stored.Owner) != nil || validateEditorWorkingCopy(stored.EditorWorkingCopy) != nil || stored.Revision != editorWorkingCopyRevision(stored.EditorWorkingCopy) {
		return EditorWorkingCopy{}, fmt.Errorf("stored editor working copy is invalid")
	}
	return stored.EditorWorkingCopy, nil
}

func (s *Store) editorWorkingCopyCount(directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	var count int
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "replace-") {
			continue
		}
		if entry.IsDir() || len(entry.Name()) != 69 || !strings.HasSuffix(entry.Name(), ".json") {
			return 0, fmt.Errorf("editor working copy storage contains an invalid entry")
		}
		count++
	}
	return count, nil
}

// SaveEditorWorkingCopy preserves unfinished text without reading, validating,
// or changing the active bundle or the separate upload draft. An empty supplied
// revision creates a new copy. Later saves must name the exact prior revision.
func (s *Store) SaveEditorWorkingCopy(owner string, copy EditorWorkingCopy) (EditorWorkingCopy, error) {
	if err := validateEditorWorkingCopyOwner(owner); err != nil {
		return EditorWorkingCopy{}, err
	}
	if err := validateEditorWorkingCopy(copy); err != nil {
		return EditorWorkingCopy{}, err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	directory, err := s.editorWorkingCopyDirectory()
	if err != nil {
		return EditorWorkingCopy{}, err
	}
	previous, err := s.readEditorWorkingCopy(directory, owner, copy.Name)
	if err != nil && !errors.Is(err, ErrNoEditorWorkingCopy) {
		return EditorWorkingCopy{}, err
	}
	if errors.Is(err, ErrNoEditorWorkingCopy) {
		if copy.Revision != "" {
			return EditorWorkingCopy{}, ErrConflict
		}
		count, err := s.editorWorkingCopyCount(directory)
		if err != nil {
			return EditorWorkingCopy{}, err
		}
		if count >= maxEditorWorkingCopies {
			return EditorWorkingCopy{}, fmt.Errorf("working copy storage is full")
		}
	} else if copy.Revision == "" || copy.Revision != previous.Revision {
		return EditorWorkingCopy{}, ErrConflict
	}
	copy.Revision = editorWorkingCopyRevision(copy)
	contents, err := json.Marshal(storedEditorWorkingCopy{Owner: owner, EditorWorkingCopy: copy})
	if err != nil {
		return EditorWorkingCopy{}, err
	}
	if len(contents) > maxEditorWorkingCopyStorage {
		return EditorWorkingCopy{}, fmt.Errorf("working copy exceeds its byte limit")
	}
	if err := writePrivate(directory, editorWorkingCopyFilename(owner, copy.Name), contents); err != nil {
		return EditorWorkingCopy{}, err
	}
	return copy, nil
}

func (s *Store) EditorWorkingCopy(owner, name string) (EditorWorkingCopy, error) {
	if err := validateEditorWorkingCopyOwner(owner); err != nil {
		return EditorWorkingCopy{}, err
	}
	if len(name) == 0 || len(name) > maxEditorWorkingCopyName || !queryName.MatchString(name) {
		return EditorWorkingCopy{}, fmt.Errorf("working copy name is invalid")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	directory, err := s.editorWorkingCopyDirectory()
	if err != nil {
		return EditorWorkingCopy{}, err
	}
	return s.readEditorWorkingCopy(directory, owner, name)
}

func (s *Store) EditorWorkingCopies(owner string) ([]EditorWorkingCopySummary, error) {
	if err := validateEditorWorkingCopyOwner(owner); err != nil {
		return nil, err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	directory, err := s.editorWorkingCopyDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	result := []EditorWorkingCopySummary{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "replace-") {
			continue
		}
		if entry.IsDir() || len(entry.Name()) != 69 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("editor working copy storage contains an invalid entry")
		}
		contents, err := readPrivate(filepath.Join(directory, entry.Name()), maxEditorWorkingCopyStorage)
		if err != nil {
			return nil, err
		}
		var stored storedEditorWorkingCopy
		if err := decodeJSON(contents, &stored); err != nil || validateEditorWorkingCopyOwner(stored.Owner) != nil || validateEditorWorkingCopy(stored.EditorWorkingCopy) != nil || stored.Revision != editorWorkingCopyRevision(stored.EditorWorkingCopy) || entry.Name() != editorWorkingCopyFilename(stored.Owner, stored.Name) {
			return nil, fmt.Errorf("stored editor working copy is invalid")
		}
		if stored.Owner == owner {
			result = append(result, EditorWorkingCopySummary{Name: stored.Name, BaseRevision: stored.BaseRevision, Revision: stored.Revision})
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

// DeleteEditorWorkingCopy requires the exact revision that was resumed. This
// keeps an old editor tab from deleting a newer saved copy.
func (s *Store) DeleteEditorWorkingCopy(owner, name, revision string) error {
	if err := validateEditorWorkingCopyOwner(owner); err != nil {
		return err
	}
	if len(name) == 0 || len(name) > maxEditorWorkingCopyName || !queryName.MatchString(name) || !validWorkingCopyRevision(revision) || revision == "" {
		return fmt.Errorf("working copy request is invalid")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	directory, err := s.editorWorkingCopyDirectory()
	if err != nil {
		return err
	}
	copy, err := s.readEditorWorkingCopy(directory, owner, name)
	if err != nil {
		return err
	}
	if copy.Revision != revision {
		return ErrConflict
	}
	return os.Remove(filepath.Join(directory, editorWorkingCopyFilename(owner, name)))
}
