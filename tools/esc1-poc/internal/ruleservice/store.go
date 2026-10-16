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
	"encoding/binary"
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
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

var ErrNoActive = errors.New("no active rule bundle")

// Status contains counts and explanations, never query answers or credentials.
// The saved revision means the last confirmed save, not the latest activation.
type Status struct {
	State            string            `json:"state"`
	Event            string            `json:"event"`
	RunID            string            `json:"run_id,omitempty"`
	Revision         string            `json:"revision"`
	Backend          string            `json:"backend,omitempty"`
	Message          string            `json:"message"`
	UpdatedAt        time.Time         `json:"updated_at"`
	Execution        *runner.Execution `json:"execution,omitempty"`
	SavedRevision    string            `json:"saved_revision,omitempty"`
	ResultsUncertain bool              `json:"results_uncertain"`
	ResultsStale     bool              `json:"results_stale"`
	History          []RunRecord       `json:"history,omitempty"`
}

// A separate history entry cannot itself contain another history.
type RunRecord struct {
	State     string            `json:"state"`
	Event     string            `json:"event"`
	RunID     string            `json:"run_id"`
	Revision  string            `json:"revision"`
	Backend   string            `json:"backend,omitempty"`
	Message   string            `json:"message"`
	UpdatedAt time.Time         `json:"updated_at"`
	Execution *runner.Execution `json:"execution,omitempty"`
}

const MaxRecentRuns = 20
const maxStoredStatusBytes = MaxRecentRuns*MaxStatusBytes + MaxStatusBytes

type Store struct {
	schemas   SchemaSnapshot
	mutex     sync.Mutex
	directory string
	active    Bundle
	plan      extension.Plan
	status    Status
	draft     Draft
}

// Open restores a validated active snapshot. Corrupt state is an error; it never
// silently becomes an empty bundle that a worker could mistake for a deletion.
func Open(directory string) (*Store, error) {
	var store = &Store{directory: directory, status: Status{State: "unavailable", Event: "not-reported", Message: "No execution report received."}}
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
		// Old experimental bundles mixed schemas and queries under filenames.
		// Do not guess which extension owns those files or rewrite saved evidence.
		if len(store.active.Files) > 0 && store.active.Schemas == nil {
			return nil, fmt.Errorf("stored rules need explicit extension associations; follow 'Convert an older experimental rules directory' in tools/esc1-poc/USAGE.md using a new state directory")
		}
		store.plan, err = validateBundle(directory, store.active)
		if err != nil {
			return nil, fmt.Errorf("validate stored rules: %w", err)
		}
	}
	contents, err = readPrivate(filepath.Join(directory, "status.json"), maxStoredStatusBytes)
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
	contents, err = readPrivate(filepath.Join(directory, "draft.json"), MaxBundleBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := decodeJSON(contents, &store.draft); err != nil {
			return nil, fmt.Errorf("read draft: %w", err)
		}
		if err := checkFileContents(store.draft.Files); err != nil {
			return nil, fmt.Errorf("read draft: %w", err)
		}
		if store.draft.Revision != draftRevision(store.draft) {
			return nil, fmt.Errorf("stored draft revision does not match its files")
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
	copied := Bundle{RetiredSchemas: map[string]SchemaAssociation{}, Schemas: cloneSchemas(bundle.Schemas), SchemaRevision: bundle.SchemaRevision, Suspended: map[string]string{}, Revision: bundle.Revision, Files: make(map[string]string, len(bundle.Files)), RetiredRelationships: append([]string(nil), bundle.RetiredRelationships...), RetiredEndpoints: copyEndpoints(bundle.RetiredEndpoints), Disabled: append([]string(nil), bundle.Disabled...), AddedAt: map[string]time.Time{}}
	for kind, owner := range bundle.RetiredSchemas {
		copied.RetiredSchemas[kind] = owner
	}
	for name, reason := range bundle.Suspended {
		copied.Suspended[name] = reason
	}
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
	var (
		change  = Change{BaseRevision: s.active.Revision, Files: map[string]string{}}
		schemas = map[string]InstalledSchema{}
	)
	// This explicit offline setup helper is used by tests and the standalone
	// lab. HTTP callers cannot supply schemas: they use Preview/Apply instead.
	for _, schema := range s.schemas.Definitions {
		schemas[schema.Namespace] = schema
	}
	for name, contents := range files {
		if extension.IsSchemaFile(name) {
			if err := uniqueJSON([]byte(contents)); err != nil {
				return Bundle{}, err
			}
			definition, err := extension.ParseDefinition([]byte(contents))
			if err != nil {
				return Bundle{}, err
			}
			digest := sha256.Sum256([]byte(definition.Schema.Namespace))
			id := int32(binary.BigEndian.Uint32(digest[:4]) & 0x7fffffff)
			schemas[definition.Schema.Namespace] = InstalledSchema{ID: id, Name: definition.Schema.Name, Namespace: definition.Schema.Namespace, Version: definition.Schema.Version, Definition: json.RawMessage(contents)}
			continue
		}
		change.Files[name] = contents
		change.Overwrite = append(change.Overwrite, name)
	}
	var snapshot = SchemaSnapshot{Definitions: []InstalledSchema{}}
	for _, schema := range schemas {
		snapshot.Definitions = append(snapshot.Definitions, schema)
	}
	if err := snapshot.validate(); err != nil {
		return Bundle{}, err
	}
	s.schemas = snapshot
	candidate, plan, _, err := s.candidate(change)
	if err != nil {
		return Bundle{}, err
	}
	return s.activate(candidate, plan)
}

func (s *Store) activate(candidate Bundle, plan extension.Plan) (Bundle, error) {
	if candidate.Revision == s.active.Revision {
		s.plan = plan
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
	return s.latestStatus()
}

// The caller holds the store lock so rules and status can be read together.
func (s *Store) latestStatus() Status {
	var result = cloneStatus(s.status)
	result.ResultsStale = result.SavedRevision == "" || result.SavedRevision != s.active.Revision || result.ResultsUncertain || result.State == "failed" || result.State == "running" || result.State == "unavailable" || result.State == "uncertain"
	if result.Execution != nil {
		result.ResultsStale = result.ResultsStale || result.Execution.CleanupPending || (s.plan.SchemaRevision != "" && result.Execution.SchemaRevision != s.plan.SchemaRevision)
		for _, query := range result.Execution.Queries {
			result.ResultsStale = result.ResultsStale || query.State == "disabled"
		}
	}
	// A lost final report must not leave a permanent success-looking spinner.
	// This is a reporting deadline, not evidence that the graph rolled back.
	if result.State == "running" && time.Since(result.UpdatedAt) > runner.DefaultLimits().RunTimeout+time.Minute {
		result.State = "unavailable"
		result.Message = "No completion report arrived before the run deadline. The save outcome is unknown."
		result.ResultsUncertain, result.ResultsStale = true, true
	}
	return result
}

func cloneStatus(status Status) Status {
	var result Status
	contents, _ := json.Marshal(status)
	_ = json.Unmarshal(contents, &result)
	return result
}

func (s *Store) Report(status Status) error {
	// History and saved-result state belong to this store, never to a sender.
	if len(status.History) != 0 || status.SavedRevision != "" || status.ResultsUncertain || status.ResultsStale {
		return fmt.Errorf("workers report one run; saved history belongs to the rule service")
	}
	if err := validateStatus(status); err != nil {
		return err
	}
	contents, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if len(contents) > MaxStatusBytes {
		return fmt.Errorf("status exceeds its byte limit")
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	if status.RunID == "" {
		status.RunID = status.Event
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	// The worker may notice a schema deletion before anybody opens the rules
	// page. Save its pause only for the exact revision it ran. A late report
	// must never disable a replacement the user has already reviewed.
	if status.Execution != nil && status.Revision == s.active.Revision {
		candidate := cloneBundle(s.active)
		for _, query := range status.Execution.Queries {
			if query.SchemaPaused {
				if _, exists := candidate.Files[query.Name]; !exists {
					return fmt.Errorf("pause names an unknown analysis")
				}
				candidate.Suspended[query.Name] = "The installed schema changed. Review before enabling this analysis."
			}
		}
		candidate.Revision = bundleRevision(candidate)
		if candidate.Revision != s.active.Revision {
			plan, err := ValidateBundle(candidate)
			if err != nil {
				return err
			}
			if _, err := s.activate(candidate, plan); err != nil {
				return err
			}
		}
	}
	status.SavedRevision, status.ResultsUncertain = s.status.SavedRevision, s.status.ResultsUncertain
	if status.Execution != nil {
		if status.Execution.Save == "succeeded" || status.Execution.Save == "unchanged" {
			status.SavedRevision, status.ResultsUncertain = status.Revision, false
		} else if status.Execution.Save == "uncertain" {
			status.ResultsUncertain = true
		}
	}
	for _, previous := range s.status.History {
		if previous.RunID != status.RunID {
			status.History = append(status.History, previous)
		}
	}
	status.History = append(status.History, RunRecord{State: status.State, Event: status.Event, RunID: status.RunID, Revision: status.Revision, Backend: status.Backend, Message: status.Message, UpdatedAt: status.UpdatedAt, Execution: status.Execution})
	if len(status.History) > MaxRecentRuns {
		status.History = status.History[len(status.History)-MaxRecentRuns:]
	}
	contents, err = json.Marshal(status)
	if err != nil {
		return err
	}
	if len(contents) > maxStoredStatusBytes {
		return fmt.Errorf("report history exceeds its byte limit")
	}
	if err := writePrivate(s.directory, "status.json", contents); err != nil {
		return err
	}
	s.status = cloneStatus(status)
	return nil
}

func validateStatus(status Status) error {
	if status.State != "success" && status.State != "failed" && status.State != "running" && status.State != "no-rules" && status.State != "uncertain" && status.State != "unavailable" {
		return fmt.Errorf("status state must be success, failed, running, no-rules, uncertain, or unavailable")
	}
	if len(status.RunID) > 180 || len(status.Backend) > 16 || len(status.History) > MaxRecentRuns {
		return fmt.Errorf("report metadata exceeds its limits")
	}
	if err := validateExecution(status.Execution); err != nil {
		return err
	}
	for _, record := range status.History {
		if err := validateStatus(Status{State: record.State, Event: record.Event, RunID: record.RunID, Revision: record.Revision, Backend: record.Backend, Message: record.Message, UpdatedAt: record.UpdatedAt, Execution: record.Execution}); err != nil {
			return err
		}
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
