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
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Draft contains only uploaded changes. It is stored separately from active
// rules and is never returned to an analysis worker. BaseRevision preserves the
// active files against which the author made their overwrite choices.
type Draft struct {
	BaseRevision string            `json:"base_revision"`
	Revision     string            `json:"revision"`
	Files        map[string]string `json:"files"`
	Overwrite    []string          `json:"overwrite,omitempty"`
}

type DraftView struct {
	Draft
	Ready           bool   `json:"ready"`
	ValidationError string `json:"validation_error,omitempty"`
}

func draftRevision(draft Draft) string {
	draft.Revision = ""
	var contents, _ = json.Marshal(draft)
	fingerprint := sha256.Sum256(contents)
	return hex.EncodeToString(fingerprint[:])
}

func (s *Store) Draft() DraftView {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.draftView()
}

// Saving checks safe filenames and bounded text, but permits missing schemas,
// missing queries, and other rule validation errors. Review reports those errors
// and activation always repeats the complete existing checks.
func (s *Store) SaveDraft(change Change) (DraftView, error) {
	var normalizeError error
	change, normalizeError = normalizeChange(change)
	if normalizeError != nil {
		return DraftView{}, normalizeError
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var (
		draft      = Draft{BaseRevision: s.active.Revision, Files: map[string]string{}}
		allowed    = map[string]bool{}
		overwrites = map[string]bool{}
		combined   = cloneBundle(s.active).Files
	)
	if change.BaseRevision != s.active.Revision || change.DraftRevision != s.draft.Revision {
		return DraftView{}, ErrConflict
	}
	if s.draft.Revision != "" && s.draft.BaseRevision != s.active.Revision {
		return DraftView{}, fmt.Errorf("%w: active rules changed since this draft was saved; download its files, discard it, and upload them again", ErrConflict)
	}
	if len(change.Files)+len(change.Remove) == 0 || len(change.Enable)+len(change.Disable) != 0 || len(change.Files) > MaxFiles || len(change.Remove) > MaxFiles || len(change.Overwrite) > MaxFiles {
		return DraftView{}, fmt.Errorf("choose draft files to upload or remove; at most %d of each", MaxFiles)
	}
	for name, contents := range s.draft.Files {
		draft.Files[name] = contents
	}
	for _, name := range s.draft.Overwrite {
		overwrites[name] = true
	}
	for _, name := range change.Overwrite {
		allowed[name] = true
	}
	for name, contents := range change.Files {
		previous, exists := draft.Files[name]
		if !exists {
			previous, exists = s.active.Files[name]
		}
		if exists && previous != contents && !allowed[name] {
			return DraftView{}, fmt.Errorf("%w: choose whether to overwrite %s in the draft", ErrConflict, name)
		}
		if allowed[name] {
			overwrites[name] = true
		}
		draft.Files[name] = contents
	}
	for _, name := range change.Remove {
		if _, exists := draft.Files[name]; !exists {
			return DraftView{}, fmt.Errorf("draft file %q does not exist", name)
		}
		delete(draft.Files, name)
		delete(overwrites, name)
	}
	if err := checkFileContents(draft.Files); err != nil {
		return DraftView{}, err
	}
	for name, contents := range draft.Files {
		combined[name] = contents
	}
	if err := checkFileContents(combined); err != nil {
		return DraftView{}, err
	}
	for name := range overwrites {
		draft.Overwrite = append(draft.Overwrite, name)
	}
	sort.Strings(draft.Overwrite)
	draft.Revision = draftRevision(draft)
	contents, err := json.Marshal(draft)
	if err != nil {
		return DraftView{}, err
	}
	if len(contents) > MaxBundleBytes {
		return DraftView{}, fmt.Errorf("draft exceeds its byte limit")
	}
	if err := writePrivate(s.directory, "draft.json", contents); err != nil {
		return DraftView{}, err
	}
	s.draft = draft
	return s.draftView(), nil
}

// Discarding a draft has no effect on active rules or graph results. Its revision
// prevents an old browser tab from discarding someone else's newer upload.
func (s *Store) DiscardDraft(revision string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if revision == "" || revision != s.draft.Revision {
		return ErrConflict
	}
	if err := os.Remove(filepath.Join(s.directory, "draft.json")); err != nil {
		return err
	}
	s.draft = Draft{}
	return nil
}

func (s *Store) draftView() DraftView {
	var view = DraftView{Draft: s.draft}
	view.Files = map[string]string{}
	for name, contents := range s.draft.Files {
		view.Files[name] = contents
	}
	view.Overwrite = append([]string(nil), s.draft.Overwrite...)
	if s.draft.Revision == "" {
		return view
	}
	if s.draft.BaseRevision != s.active.Revision {
		view.ValidationError = "Active rules changed since this draft was saved. Download its files, discard it, and upload them again to review against the current rules."
		return view
	}
	_, _, _, err := s.candidate(Change{BaseRevision: s.active.Revision, DraftRevision: s.draft.Revision})
	if err != nil {
		view.ValidationError = err.Error()
	} else {
		view.Ready = true
	}
	return view
}
