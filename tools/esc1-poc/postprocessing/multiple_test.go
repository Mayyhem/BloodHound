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
	"reflect"
	"strings"
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
		change := ruleservice.Change{BaseRevision: bundle.Revision, Disable: []string{"ad_users.cypher"}}
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

func registeredSchemaName(t *testing.T, payload []byte) string {
	t.Helper()
	var definition struct {
		Schema struct {
			Name string `json:"name"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(payload, &definition); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"post_processing", "existing_edge_policy", "external_node_kinds", "external_relationship_kinds", "endpoints"} {
		if strings.Contains(string(payload), `"`+field+`"`) {
			t.Fatal("proposal fields leaked into native schema", field)
		}
	}
	return definition.Schema.Name
}

func TestServiceRegistersEachSchemaBeforeExecutingCombinedPlan(t *testing.T) {
	var (
		service, client, now, calls = newServiceTest(t)
		registrations               []string
		original                    = service.execute
	)
	client.bundle = multiSchemaServiceBundle(t, true)
	service.configuration.RegisterSchema = func(_ context.Context, payload []byte) error {
		registrations = append(registrations, registeredSchemaName(t, payload))
		if *calls != 0 {
			t.Fatal("schema registration followed query execution")
		}
		return nil
	}
	service.execute = func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		if !reflect.DeepEqual(registrations, []string{"active_directory", "cyberark"}) || len(plan.Definitions) != 2 || len(plan.Queries) != 3 {
			t.Fatal("runner did not receive registered combined plan", registrations, plan)
		}
		for _, query := range plan.Queries {
			expectedDisabled := query.Name == "ad_users.cypher" || query.Name == "cyberark_links.cypher"
			if (query.DisabledReason != "") != expectedDisabled {
				t.Fatal("runner lost selective disabled state", query)
			}
		}
		return original(ctx, plan, options)
	}
	if committed, err := service.AfterAnalysis(context.Background()); err != nil || !committed {
		t.Fatal("first run failed", committed, err)
	}
	*now = now.Add(time.Minute)
	if committed, err := service.AfterAnalysis(context.Background()); err != nil || !committed {
		t.Fatal("second run failed", committed, err)
	}
	if len(registrations) != 2 || *calls != 2 {
		t.Fatal("unchanged schemas were registered again", registrations, *calls)
	}
}

func TestServiceRetriesAllSchemasAfterPartialRegistrationFailure(t *testing.T) {
	var (
		service, client, now, calls = newServiceTest(t)
		registrations               []string
		failSecond                  = true
		original                    = service.execute
		registrationError           = errors.New("second schema rejected")
	)
	client.bundle = multiSchemaServiceBundle(t, false)
	service.configuration.RegisterSchema = func(_ context.Context, payload []byte) error {
		name := registeredSchemaName(t, payload)
		registrations = append(registrations, name)
		if failSecond && name == "cyberark" {
			return registrationError
		}
		return nil
	}
	service.execute = func(ctx context.Context, plan extension.Plan, options runner.Options) (runner.Report, error) {
		if !reflect.DeepEqual(registrations, []string{"active_directory", "cyberark", "active_directory", "cyberark"}) {
			t.Fatal("executed before complete registration retry", registrations)
		}
		return original(ctx, plan, options)
	}
	if committed, err := service.AfterAnalysis(context.Background()); committed || !errors.Is(err, registrationError) {
		t.Fatal("registration failure was hidden", committed, err)
	}
	if *calls != 0 || len(service.registeredSchema) != 0 || service.pending == nil {
		t.Fatal("partial registration was cached or executed", *calls, service.pending)
	}
	failSecond = false
	if committed, err := service.Retry(context.Background()); committed || err != nil {
		t.Fatal("retry ignored registration backoff", committed, err)
	}
	if len(registrations) != 2 || *calls != 0 {
		t.Fatal("retried before backoff elapsed", registrations, *calls)
	}
	*now = now.Add(time.Minute)
	if committed, err := service.Retry(context.Background()); err != nil || !committed {
		t.Fatal("registration retry failed", committed, err)
	}
	if *calls != 1 || len(service.registeredSchema) == 0 || client.fetches != 1 || service.pending != nil {
		t.Fatal("retry did not complete pinned bundle", *calls, client.fetches, service.pending)
	}
	*now = now.Add(time.Minute)
	if _, err := service.AfterAnalysis(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 4 || *calls != 2 {
		t.Fatal("complete schema list was not cached", registrations, *calls)
	}
}
