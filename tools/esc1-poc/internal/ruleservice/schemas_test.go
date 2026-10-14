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

package ruleservice_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

func installedSchema(t *testing.T, id int32, namespace, version string) ruleservice.InstalledSchema {
	t.Helper()
	var definition = extension.Definition{
		Schema:    extension.Metadata{Name: namespace, DisplayName: namespace, Namespace: namespace, Version: version},
		NodeKinds: []extension.NodeKind{{Name: namespace + "_Account"}},
		RelationshipKinds: []extension.RelationshipKind{
			{Name: namespace + "_Link", CompatibleKinds: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}},
			{Name: namespace + "_Combined", CompatibleKinds: []extension.Endpoint{{SourceKind: "User", TargetKind: "Domain"}}},
		},
	}
	contents, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	return ruleservice.InstalledSchema{ID: id, Name: namespace, Namespace: namespace, Version: version, Definition: contents}
}

func scopedRule(namespace, version, dependencies, body string) string {
	return "// namespace: " + namespace + "\n// version: " + version + "\n// depends-on: " + dependencies + "\n" + body
}

func scopedProducer(namespace, version string) string {
	return scopedRule(namespace, version, "none", "RETURN 'source' AS source, 'target' AS target, '"+namespace+"_Link' AS kind")
}

func TestIndependentUploadsVersionReviewAndRetainedDeletion(t *testing.T) {
	var (
		store    = open(t)
		snapshot = ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}}
		active   ruleservice.Bundle
	)
	// Identical filenames arrive separately, after each independent schema upload.
	for index, namespace := range []string{"PoC_AD", "PoC_Vault", "PoC_CyberArk"} {
		snapshot.Definitions = append(snapshot.Definitions, installedSchema(t, int32(index+1), namespace, "v1.0.0"))
		if err := store.RefreshSchemas(snapshot); err != nil {
			t.Fatal(err)
		}
		active = applyChange(t, store, ruleservice.Change{BaseRevision: active.Revision, Files: map[string]string{"users.cypher": scopedProducer(namespace, "v1.0.3")}})
	}
	if len(active.Files) != 3 || active.Files["PoC_AD/users.cypher"] == active.Files["PoC_Vault/users.cypher"] {
		t.Fatal("same filenames collided")
	}
	consumer := scopedRule("PoC_CyberArk", "v1.0.0", "query:PoC_AD/users>=v1.0.3,<v2.0.0 as directory_users", "UNWIND $directory_users AS person RETURN person.source AS source, person.target AS target, 'PoC_CyberArk_Combined' AS kind")
	active = applyChange(t, store, ruleservice.Change{BaseRevision: active.Revision, Files: map[string]string{"links.cypher": consumer}})
	// A schema-only upgrade changes the actual schema version, not the query's version.
	snapshot.Definitions[0] = installedSchema(t, 1, "PoC_AD", "v1.1.0")
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	view := crossPlatformViews(store.View())
	if view["PoC_AD/users.cypher"].Version != "v1.0.3" || view["PoC_AD/users.cypher"].SchemaVersion != "v1.1.0" || !view["PoC_CyberArk/links.cypher"].Enabled {
		t.Fatal("compatible schema upgrade broke unchanged query")
	}
	// A draft is never selected as latest; this range rejects its proposed major update.
	draft, err := store.SaveDraft(ruleservice.Change{BaseRevision: active.Revision, Files: map[string]string{"users.cypher": scopedProducer("PoC_AD", "v2.0.0")}, Overwrite: []string{"users.cypher"}})
	if err != nil || draft.Ready || !strings.Contains(draft.ValidationError, "does not satisfy") {
		t.Fatalf("incompatible draft: %+v %v", draft, err)
	}
	unchanged, _ := store.Active()
	if unchanged.Files["PoC_AD/users.cypher"] != active.Files["PoC_AD/users.cypher"] {
		t.Fatal("draft replaced active provider")
	}
	if err := store.DiscardDraft(draft.Revision); err != nil {
		t.Fatal(err)
	}
	// Deletion preserves files and pauses only the affected chain.
	snapshot.Definitions = snapshot.Definitions[1:]
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	retained, _ := store.Active()
	view = crossPlatformViews(store.View())
	if len(retained.Files) != 4 || view["PoC_AD/users.cypher"].Enabled || view["PoC_CyberArk/links.cypher"].Enabled || !view["PoC_Vault/users.cypher"].Enabled || !view["PoC_CyberArk/users.cypher"].Enabled {
		t.Fatal("deletion lost files or paused independent rules", view)
	}
	// Reinstalling even the same version gets a new native record; require review.
	snapshot.Definitions = append(snapshot.Definitions, installedSchema(t, 4, "PoC_AD", "v1.1.0"))
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	if crossPlatformViews(store.View())["PoC_AD/users.cypher"].Enabled {
		t.Fatal("reinstall silently resumed retained analysis")
	}
	retained, _ = store.Active()
	active = applyChange(t, store, ruleservice.Change{BaseRevision: retained.Revision, Enable: []string{"PoC_AD/users.cypher", "PoC_CyberArk/links.cypher"}})
	if !crossPlatformViews(store.View())["PoC_CyberArk/links.cypher"].Enabled || len(active.Files) != 4 {
		t.Fatal("reviewed enable did not restore the complete chain")
	}
}

func TestAnalysisBeforeSchemaStaysDraftAndSchemaChangesInvalidateReview(t *testing.T) {
	var store = open(t)
	if err := store.RefreshSchemas(ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}}); err != nil {
		t.Fatal(err)
	}
	draft, err := store.SaveDraft(ruleservice.Change{Files: map[string]string{"users.cypher": scopedProducer("PoC_AD", "v1.0.0")}})
	if err != nil || draft.Ready || !strings.Contains(draft.ValidationError, "OpenGraph Management") {
		t.Fatalf("draft before schema: %+v %v", draft, err)
	}
	snapshot := ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{installedSchema(t, 1, "PoC_AD", "v1.0.0")}}
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	change := ruleservice.Change{DraftRevision: draft.Revision}
	preview, err := store.Preview(change)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Definitions[0] = installedSchema(t, 1, "PoC_AD", "v1.1.0")
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	change.ConfirmRevision = preview.Revision
	if _, err := store.Apply(change); err == nil {
		t.Fatal("stale schema review activated")
	}
	active := applyChange(t, store, ruleservice.Change{DraftRevision: draft.Revision})
	if len(active.Files) != 1 || len(active.Schemas) != 1 {
		t.Fatal("separate schema upload did not satisfy draft")
	}
}

func TestOrdinaryDisableDoesNotLatchSchemaPause(t *testing.T) {
	store := open(t)
	snapshot := ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{installedSchema(t, 1, "PoC_AD", "v1.0.0")}}
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	consumer := scopedRule("PoC_AD", "v1.0.0", "query:users", "UNWIND $users AS person RETURN person.source AS source, person.target AS target, 'PoC_AD_Combined' AS kind")
	active := applyChange(t, store, ruleservice.Change{Files: map[string]string{"users.cypher": scopedProducer("PoC_AD", "v1.0.0"), "links.cypher": consumer}})
	active = applyChange(t, store, ruleservice.Change{BaseRevision: active.Revision, Disable: []string{"PoC_AD/users.cypher"}})
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	current, _ := store.Active()
	if len(current.Suspended) != 0 {
		t.Fatal("ordinary disabled dependency was latched as a schema failure")
	}
	applyChange(t, store, ruleservice.Change{BaseRevision: current.Revision, Enable: []string{"PoC_AD/users.cypher"}})
	if !crossPlatformViews(store.View())["PoC_AD/links.cypher"].Enabled {
		t.Fatal("consumer did not resume when its provider was enabled")
	}
}

// A changed RETURN can break an unchanged consumer even when both declared
// versions are allowed. Check direct shared-row column use before activation.
func TestSharedColumnRenameRequiresConsumerUpdate(t *testing.T) {
	var store = open(t)
	if err := store.RefreshSchemas(ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{installedSchema(t, 1, "PoC_AD", "v1.0.0")}}); err != nil {
		t.Fatal(err)
	}
	provider := scopedRule("PoC_AD", "v1.0.0", "", "// depends-on: none\nRETURN 'USER' AS user_id")
	consumer := scopedRule("PoC_AD", "v1.0.0", "", "// depends-on: query:users as directory_users\nUNWIND $directory_users AS person RETURN person.user_id AS source, 'DOMAIN' AS target, 'PoC_AD_Link' AS kind")
	initial := applyChange(t, store, ruleservice.Change{Files: map[string]string{"users.cypher": provider, "links.cypher": consumer}})
	replacement := strings.ReplaceAll(provider, "AS user_id", "AS identity")
	draft, err := store.SaveDraft(ruleservice.Change{BaseRevision: initial.Revision, Files: map[string]string{"users.cypher": replacement}, Overwrite: []string{"users.cypher"}})
	if err != nil || draft.Ready || !strings.Contains(draft.ValidationError, "user_id") {
		t.Fatalf("rename must stay an inactive draft: %+v %v", draft, err)
	}
	if active, _ := store.Active(); active.Revision != initial.Revision {
		t.Fatal("rename changed active provider")
	}
}

func TestMissingSchemaDefersCleanupAndWorkerPauseSurvivesRestart(t *testing.T) {
	var directory = t.TempDir()
	store, err := ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := store.Update(files(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ruleservice.ValidateInstalledBundle(initial, ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}})
	if err != nil {
		t.Fatal(err)
	}
	details := &runner.Execution{Calculation: "skipped", Save: "unchanged", Phase: "complete"}
	for index, query := range plan.Queries {
		details.Queries = append(details.Queries, runner.QueryReport{Name: query.Name, Stage: index + 1, State: "disabled", Reason: query.DisabledReason, SchemaPaused: query.SchemaPaused})
	}
	if err := store.Report(ruleservice.Status{State: "success", Event: "analysis-completed", Revision: initial.Revision, Execution: details}); err != nil {
		t.Fatal(err)
	}
	store, err = ruleservice.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	paused, _ := store.Active()
	if len(paused.Suspended) != len(initial.Files) || !store.LatestStatus().ResultsStale {
		t.Fatal("worker pause was not retained")
	}
	if err := store.RefreshSchemas(ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}}); err != nil {
		t.Fatal(err)
	}
	paused, _ = store.Active()
	removed := applyChange(t, store, ruleservice.Change{BaseRevision: paused.Revision, Remove: []string{"PoC_Vault/account_users.cypher"}})
	plan, err = ruleservice.ValidateInstalledBundle(removed, ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{}})
	if err != nil || !plan.CleanupPending || len(plan.RetiredRelationships) != 0 || len(removed.RetiredRelationships) != 1 {
		t.Fatalf("cleanup must wait without forgetting history: %+v %v", plan, err)
	}
}

func TestRenamedOutputRetainsItsCleanupOwner(t *testing.T) {
	var (
		store    = open(t)
		snapshot = ruleservice.SchemaSnapshot{Definitions: []ruleservice.InstalledSchema{installedSchema(t, 1, "PoC_AD", "v1.0.0")}}
	)
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	active := applyChange(t, store, ruleservice.Change{Files: map[string]string{"users.cypher": scopedProducer("PoC_AD", "v1.0.0")}})
	// Updating the same installed schema can remove the old kind declaration.
	// Its previous write boundaries and owner must still authorize cleanup.
	snapshot.Definitions[0].Version = "v1.1.0"
	snapshot.Definitions[0].Definition = json.RawMessage(strings.ReplaceAll(strings.ReplaceAll(string(snapshot.Definitions[0].Definition), "v1.0.0", "v1.1.0"), "PoC_AD_Link", "PoC_AD_Renamed"))
	if err := store.RefreshSchemas(snapshot); err != nil {
		t.Fatal(err)
	}
	active, _ = store.Active()
	active = applyChange(t, store, ruleservice.Change{BaseRevision: active.Revision,
		Files:     map[string]string{"users.cypher": strings.ReplaceAll(scopedProducer("PoC_AD", "v1.1.0"), "PoC_AD_Link", "PoC_AD_Renamed")},
		Overwrite: []string{"PoC_AD/users.cypher"}, Enable: []string{"PoC_AD/users.cypher"}})
	plan, err := ruleservice.ValidateInstalledBundle(active, snapshot)
	if err != nil || plan.CleanupPending || len(plan.RetiredRelationships) != 1 || plan.RetiredRelationships[0] != "PoC_AD_Link" {
		t.Fatalf("renamed output lost safe cleanup: %+v %v", plan.RetiredRelationships, err)
	}
	snapshot.Definitions[0].ID = 2
	plan, err = ruleservice.ValidateInstalledBundle(active, snapshot)
	if err != nil || !plan.CleanupPending || len(plan.RetiredRelationships) != 0 {
		t.Fatal("a different installation must not silently inherit old cleanup")
	}
}
