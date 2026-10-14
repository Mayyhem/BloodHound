// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package postprocessing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func multiSchemaServiceBundle(t *testing.T, disabled bool) ruleservice.Bundle {
	t.Helper()
	var (
		directory = filepath.Join("..", "examples", "cross-platform", "extension")
		files     = make(map[string]string)
	)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(contents)
	}
	store, err := ruleservice.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.Update(files)
	if err != nil {
		t.Fatal(err)
	}
	if disabled {
		change := ruleservice.Change{BaseRevision: bundle.Revision, Disable: []string{"PoC_AD/ad_users.cypher"}}
		preview, err := store.Preview(change)
		if err != nil {
			t.Fatal(err)
		}
		change.ConfirmRevision = preview.Revision
		bundle, err = store.Apply(change)
		if err != nil {
			t.Fatal(err)
		}
	}
	return bundle
}

func TestServiceChecksCombinedInstalledSchemasAndPausesDeletedExtension(t *testing.T) {
	service, client, _, _ := newServiceTest(t)
	client.bundle = multiSchemaServiceBundle(t, false)
	snapshot := ruleservice.SchemaSnapshot{Definitions: client.bundle.Schemas[1:]}
	// Remove Directory, independently of the saved rule files.
	snapshot.Definitions = nil
	for _, schema := range client.bundle.Schemas {
		if schema.Namespace != "PoC_AD" {
			snapshot.Definitions = append(snapshot.Definitions, schema)
		}
	}
	service.configuration.ReadSchemas = func(context.Context) ([]byte, error) { return json.Marshal(snapshot) }
	service.execute = func(_ context.Context, plan extension.Plan, _ runner.Options) (runner.Report, error) {
		if len(plan.Queries) != 3 {
			t.Fatal("retained files disappeared")
		}
		for _, query := range plan.Queries {
			wantPaused := query.Name != "PoC_CyberArk/cyberark_accounts.cypher"
			if (query.DisabledReason != "") != wantPaused {
				t.Fatalf("wrong pause for %s: %s", query.Name, query.DisabledReason)
			}
		}
		return runner.Report{Committed: true}, nil
	}
	if _, err := service.AfterAnalysis(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Definitions) != 1 {
		t.Fatal("analysis recreated deleted schema")
	}
}

func TestServiceRetriesSchemaReadWithoutChangingPinnedFiles(t *testing.T) {
	service, client, now, calls := newServiceTest(t)
	original := service.configuration.ReadSchemas
	service.configuration.ReadSchemas = func(context.Context) ([]byte, error) { return nil, errors.New("schema source unavailable") }
	if _, err := service.AfterAnalysis(t.Context()); err == nil || *calls != 0 {
		t.Fatal("read failure allowed execution")
	}
	service.configuration.ReadSchemas = original
	if _, err := service.Retry(t.Context()); err != nil || *calls != 0 {
		t.Fatal("retry ignored backoff")
	}
	*now = now.Add(time.Minute)
	if committed, err := service.Retry(t.Context()); err != nil || !committed || *calls != 1 || client.fetches != 1 {
		t.Fatal("retry changed pinned files", err)
	}
}
