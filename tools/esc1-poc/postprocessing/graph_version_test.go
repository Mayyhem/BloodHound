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
	"testing"
	"time"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/extension"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/runner"
)

func TestGraphVersionChangesForOrdinaryAndPrivateWrites(t *testing.T) {
	gate := NewGate()
	initial := gate.Version()
	if initial != gate.Version() {
		t.Fatal("reading the graph version changed it")
	}
	if initial == NewGate().Version() {
		t.Fatal("new gate reused a restart epoch")
	}
	_, release, err := gate.EnterWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gate.Version().Revision != initial.Revision+1 {
		t.Fatal("ordinary admitted write did not change graph version")
	}
	release()
	afterOrdinary := gate.Version()
	if err := gate.Alone(context.Background(), time.Second, func(turnContext context.Context) error {
		_, privateRelease, err := gate.EnterWrite(turnContext)
		if err != nil {
			return err
		}
		privateRelease()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if gate.Version().Revision != afterOrdinary.Revision+1 {
		t.Fatal("private normal write did not change graph version")
	}
}

func TestTemporaryGraphChangesOnlyApplyInsideItsExclusiveTurn(t *testing.T) {
	gate := NewGate()
	initial := gate.Version()
	if err := gate.Alone(context.Background(), time.Second, func(turnContext context.Context) error {
		temporaryContext := TemporaryGraphChanges(turnContext)
		_, release, err := gate.EnterWrite(temporaryContext)
		if err != nil {
			return err
		}
		release()
		if gate.Version() != initial {
			t.Fatal("rollback-only write changed graph version")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ordinaryContext := TemporaryGraphChanges(context.Background())
	_, release, err := gate.EnterWrite(ordinaryContext)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if gate.Version().Revision != initial.Revision+1 {
		t.Fatal("ordinary write was incorrectly marked temporary")
	}
}

func TestWithAnalysisInvalidatesGraphVersionBeforeNativeWork(t *testing.T) {
	service := &Service{gate: NewGate()}
	initial := service.gate.Version()
	if err := service.WithAnalysis(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if service.gate.Version().Revision != initial.Revision+1 {
		t.Fatal("normal analysis turn did not change graph version")
	}
}

func TestPublicationAdvancesVersionWithoutAnOrdinaryDriverWrite(t *testing.T) {
	service, _, _, _ := newServiceTest(t)
	before := service.gate.Version()
	service.execute = func(context.Context, extension.Plan, runner.Options) (runner.Report, error) {
		return runner.Report{Committed: true}, nil
	}
	committed, err := service.AfterAnalysis(t.Context())
	if err != nil || !committed {
		t.Fatalf("publication=%v error=%v", committed, err)
	}
	if service.gate.Version() == before {
		t.Fatal("direct PostgreSQL publication did not invalidate older editor work")
	}
}
