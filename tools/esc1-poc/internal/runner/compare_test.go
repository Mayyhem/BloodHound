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

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

type comparisonDeadlineTransaction struct {
	blockedStream string
	streams       []string
	deadline      time.Time
	writes        int
	commits       int
}

func (s *comparisonDeadlineTransaction) Stream(ctx context.Context, query string, parameters map[string]any, consume func(map[string]any) error) error {
	var streamName = "native"
	if strings.Contains(query, "edge:PoC_ADCSESC1") {
		streamName = "owned"
	}
	s.streams = append(s.streams, streamName)
	// Leave a real partial answer before waiting, so cleanup must remove files.
	if err := consume(map[string]any{"source": "principal", "target": "domain"}); err != nil {
		return err
	}
	if streamName == s.blockedStream {
		var present bool
		s.deadline, present = ctx.Deadline()
		if !present {
			return fmt.Errorf("comparison stream has no deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (s *comparisonDeadlineTransaction) Read(context.Context, string, map[string]any) (Rows, error) {
	return nil, fmt.Errorf("comparison must use the streaming interface")
}

func (s *comparisonDeadlineTransaction) Write(context.Context, string, map[string]any) (Rows, error) {
	s.writes++
	return nil, fmt.Errorf("comparison must not write")
}

func (s *comparisonDeadlineTransaction) Commit(context.Context) error {
	s.commits++
	return fmt.Errorf("comparison must not commit")
}

func (s *comparisonDeadlineTransaction) Rollback(context.Context) error { return nil }

func TestComparePublishedDeadlines(t *testing.T) {
	for _, deadlineName := range []string{"run", "query"} {
		for _, streamName := range []string{"owned", "native"} {
			t.Run(deadlineName+"/"+streamName, func(t *testing.T) {
				var (
					limits      = DefaultLimits()
					transaction = &comparisonDeadlineTransaction{blockedStream: streamName}
					directory   = t.TempDir()
					budget      = 200 * time.Millisecond
					started     = time.Now()
				)
				limits.RunTimeout, limits.QueryTimeout = 3*time.Second, 3*time.Second
				if deadlineName == "run" {
					limits.RunTimeout = budget
				} else {
					limits.QueryTimeout = budget
				}
				// A fallback keeps a broken deadline implementation from hanging tests.
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				_, err := ComparePublished(ctx, transaction, directory, limits)
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected comparison deadline error: %v", err)
				}
				if len(transaction.streams) == 0 || transaction.streams[len(transaction.streams)-1] != streamName {
					t.Fatalf("deadline did not reach intended stream: %v", transaction.streams)
				}
				if duration := transaction.deadline.Sub(started); duration > budget+time.Second {
					t.Fatalf("stream used fallback deadline instead of configured %s deadline: %s", deadlineName, duration)
				}
				if transaction.writes != 0 || transaction.commits != 0 {
					t.Fatal("timed-out comparison wrote or committed graph data")
				}
				files, err := os.ReadDir(directory)
				if err != nil || len(files) != 0 {
					t.Fatalf("timed-out comparison left temporary answers: %v, %v", files, err)
				}
			})
		}
	}
}
