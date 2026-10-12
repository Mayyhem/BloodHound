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

package bloodhound

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// WaitUntilIdle checks background work as well as uploads. BloodHound requests
// a new full analysis every time it starts. An old completed upload therefore
// cannot prove its current calculated relationships are ready to read.
// Call this after checking upload history and immediately before stopping the
// dedicated application. No other clients may queue work during this interval.
func (s *Client) WaitUntilIdle(ctx context.Context) error {
	for {
		var (
			pending struct {
				Data struct {
					RequestType *string `json:"request_type"`
				} `json:"data"`
			}
			processing struct {
				Data struct {
					Status    string    `json:"status"`
					Started   time.Time `json:"last_analysis_run_at"`
					Completed time.Time `json:"last_complete_analysis_at"`
				} `json:"data"`
			}
		)
		// Read the queue first: a request taken from the queue will then be
		// visible as running work (or a newly completed calculation) below.
		if err := s.request(ctx, http.MethodGet, "/api/v2/analysis/status", "", "", nil, &pending); err != nil {
			return err
		}
		if pending.Data.RequestType == nil {
			return fmt.Errorf("BloodHound analysis queue status is missing")
		}
		if *pending.Data.RequestType != "" && *pending.Data.RequestType != "analysis" {
			return fmt.Errorf("BloodHound has a deletion or unknown background request; existing results were kept")
		}
		if err := s.request(ctx, http.MethodGet, "/api/v2/datapipe/status", "", "", nil, &processing); err != nil {
			return err
		}
		switch processing.Data.Status {
		case "idle":
			if *pending.Data.RequestType == "" {
				if processing.Data.Started.IsZero() || processing.Data.Completed.IsZero() || processing.Data.Completed.Before(processing.Data.Started) {
					return fmt.Errorf("BloodHound's most recent background analysis has no successful completion; existing results were kept")
				}
				return nil
			}
		case "starting", "ingesting", "analyzing", "optimizing", "pruning":
			// Wait for the ordinary background steps to finish.
		default:
			return fmt.Errorf("BloodHound background status %q cannot authorize publication", processing.Data.Status)
		}
		timer := time.NewTimer(s.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
