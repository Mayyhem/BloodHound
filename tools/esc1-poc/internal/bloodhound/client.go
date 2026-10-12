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

// Package bloodhound uploads collection files and waits for native analysis.
package bloodhound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	token        string
	httpClient   *http.Client
	pollInterval time.Duration
}

type Receipt struct {
	JobID       int64  `json:"job_id"`
	Status      string `json:"status"`
	CompletedAt string `json:"completed_at"`
}

// Job follows the server model, including partially failed files, which older
// OpenAPI descriptions omit. A partially ingested file cannot authorize cleanup.
// Pointers let us distinguish an explicit zero failures from a missing answer;
// nil means the server did not supply that count.
type Job struct {
	ID                 int64  `json:"id"`
	Status             *int   `json:"status"`
	StatusMessage      string `json:"status_message"`
	EndTime            string `json:"end_time"`
	TotalFiles         *int   `json:"total_files"`
	FailedFiles        *int   `json:"failed_files"`
	PartialFailedFiles *int   `json:"partial_failed_files"`
}

func New(baseURL, token string) (*Client, error) {
	return newClient(baseURL, token, false)
}

// NewContainer connects to a numeric private address after the caller verifies
// the Docker container and network that own it.
func NewContainer(baseURL, token string) (*Client, error) {
	return newClient(baseURL, token, true)
}

func newClient(baseURL, token string, container bool) (*Client, error) {
	var (
		parsedURL, err = url.Parse(baseURL)
		transport      = http.DefaultTransport.(*http.Transport).Clone()
		allowedHost    bool
	)
	if err != nil {
		return nil, fmt.Errorf("parse BloodHound URL: %w", err)
	}
	if container {
		var address = net.ParseIP(parsedURL.Hostname())
		allowedHost = address != nil && address.IsPrivate()
	} else {
		allowedHost = parsedURL.Hostname() == "bloodhound.localhost" || parsedURL.Hostname() == "127.0.0.1"
	}
	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || !allowedHost || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.ForceQuery || parsedURL.Fragment != "" || parsedURL.Opaque != "" || (parsedURL.Path != "" && parsedURL.Path != "/") {
		if container {
			return nil, fmt.Errorf("BloodHound container URL must be an http(s) origin at a numeric private IP address")
		}
		return nil, fmt.Errorf("BloodHound URL must be an http(s) origin at bloodhound.localhost or 127.0.0.1")
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("BloodHound bearer token is required and must fit on one line")
	}
	// Ignore proxy environment variables: credentials belong only to this verified server.
	transport.Proxy = nil
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, pollInterval: time.Second, httpClient: &http.Client{
		Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("BloodHound redirects are not allowed")
		},
	}}, nil
}

func (s *Client) request(ctx context.Context, method, path, contentType, filename string, payload []byte, result any) error {
	var (
		request, err = http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(payload))
		response     *http.Response
	)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+s.token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if filename != "" {
		request.Header.Set("X-File-Upload-Name", filename)
	}
	response, err = s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("BloodHound %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// The status is enough to diagnose access errors; don't print server bodies
		// that may accidentally echo credentials or sensitive collection content.
		return fmt.Errorf("BloodHound %s %s: HTTP %d", method, path, response.StatusCode)
	}
	if result != nil {
		if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(result); err != nil {
			return fmt.Errorf("decode BloodHound response: %w", err)
		}
	}
	return nil
}

// UploadAndWait validates every file before creating a server job. Keeping those
// bytes for the upload means a file changed later cannot bypass validation.
func (s *Client) UploadAndWait(ctx context.Context, paths []string) (Receipt, error) {
	var (
		payloads     = make([][]byte, len(paths))
		contentTypes = make([]string, len(paths))
		started      struct {
			Data Job `json:"data"`
		}
		receipt Receipt
		err     error
	)
	if len(paths) == 0 {
		return receipt, fmt.Errorf("at least one collection file is required")
	}
	for index, path := range paths {
		if err = ctx.Err(); err != nil {
			return receipt, err
		}
		payloads[index], contentTypes[index], err = readCollection(path)
		if err != nil {
			return receipt, fmt.Errorf("validate %s: %w", path, err)
		}
	}
	if err = s.request(ctx, http.MethodPost, "/api/v2/file-upload/start", "", "", nil, &started); err != nil {
		return receipt, err
	}
	receipt.JobID = started.Data.ID
	if receipt.JobID <= 0 {
		return receipt, fmt.Errorf("BloodHound returned no valid upload job ID")
	}
	for index, path := range paths {
		if err = s.request(ctx, http.MethodPost, fmt.Sprintf("/api/v2/file-upload/%d", receipt.JobID), contentTypes[index], filepath.Base(path), payloads[index], nil); err != nil {
			return receipt, err
		}
	}
	if err = s.request(ctx, http.MethodPost, fmt.Sprintf("/api/v2/file-upload/%d/end", receipt.JobID), "", "", nil, nil); err != nil {
		return receipt, err
	}
	return s.Wait(ctx, receipt.JobID)
}

// Status uses the list endpoint because the server has no single-job GET route.
func (s *Client) Status(ctx context.Context, jobID int64) (Job, error) {
	var (
		response struct {
			Data []Job `json:"data"`
		}
		err error
	)
	if jobID <= 0 {
		return Job{}, fmt.Errorf("upload job ID must be positive")
	}
	err = s.request(ctx, http.MethodGet, fmt.Sprintf("/api/v2/file-upload?id=eq:%d&limit=1", jobID), "", "", nil, &response)
	if err != nil {
		return Job{}, err
	}
	if len(response.Data) != 1 || response.Data[0].ID != jobID {
		return Job{}, fmt.Errorf("upload job %d was not found unambiguously", jobID)
	}
	return response.Data[0], nil
}

// Wait returns success only after ingestion AND native analysis finish. All
// failure results leave publication decisions to the caller; this client never
// changes experimental relationships or deletes a failed upload job.
func (s *Client) Wait(ctx context.Context, jobID int64) (Receipt, error) {
	var (
		receipt = Receipt{JobID: jobID}
		job     Job
		err     error
		timer   *time.Timer
	)
	for {
		job, err = s.Status(ctx, jobID)
		if err != nil {
			return receipt, err
		}
		receipt.Status = statusName(job.Status)
		receipt.CompletedAt = job.EndTime
		if job.Status == nil {
			return receipt, fmt.Errorf("upload job %d has no status", jobID)
		}
		switch *job.Status {
		case 2:
			if err = validateCompletedJob(job); err != nil {
				return receipt, err
			}
			return receipt, nil
		case 0, 1, 6, 7:
			// These are the only server states that can still become successful.
		default:
			return receipt, fmt.Errorf("upload job %d ended with %s: %s", jobID, receipt.Status, job.StatusMessage)
		}
		timer = time.NewTimer(s.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return receipt, ctx.Err()
		case <-timer.C:
		}
	}
}

func statusName(status *int) string {
	if status == nil {
		return "unknown"
	}
	switch *status {
	case 0:
		return "ready"
	case 1:
		return "running"
	case 2:
		return "complete"
	case 3:
		return "canceled"
	case 4:
		return "timedout"
	case 5:
		return "failed"
	case 6:
		return "ingesting"
	case 7:
		return "analyzing"
	case 8:
		return "partiallycomplete"
	default:
		return "unknown"
	}
}

func validateCompletedJob(job Job) error {
	if job.ID <= 0 {
		return fmt.Errorf("upload job has no valid ID")
	}
	if job.Status == nil || *job.Status != 2 {
		return fmt.Errorf("upload job %d is %s rather than complete", job.ID, statusName(job.Status))
	}
	if job.TotalFiles == nil || job.FailedFiles == nil || job.PartialFailedFiles == nil {
		return fmt.Errorf("upload job %d is missing file counts", job.ID)
	}
	if *job.TotalFiles <= 0 || *job.FailedFiles != 0 || *job.PartialFailedFiles != 0 {
		return fmt.Errorf("upload job %d is complete but empty or has failed files", job.ID)
	}
	if _, err := time.Parse(time.RFC3339Nano, job.EndTime); err != nil || strings.HasPrefix(job.EndTime, "0001-") {
		return fmt.Errorf("upload job %d has no valid completion time", job.ID)
	}
	return nil
}

// CheckAllJobsComplete is a conservative rule for the dedicated experiment lab.
// An old success cannot authorize cleanup after another upload failed or is
// still running. Historical failures also block this check; use a fresh lab
// rather than hiding a failure by selecting an earlier successful job.
func (s *Client) CheckAllJobsComplete(ctx context.Context) error {
	var (
		observed      int
		previousID    int64
		expectedCount *int
	)
	// This bound prevents a broken or endlessly changing server pagination response
	// from keeping the check running forever, even without a context deadline.
	for page := 0; page < 10000; page++ {
		var response struct {
			Data  []Job `json:"data"`
			Count *int  `json:"count"`
			Skip  *int  `json:"skip"`
			Limit *int  `json:"limit"`
		}
		if err := s.request(ctx, http.MethodGet, fmt.Sprintf("/api/v2/file-upload?sort_by=id&skip=%d&limit=100", observed), "", "", nil, &response); err != nil {
			return err
		}
		if len(response.Data) > 100 || (response.Skip != nil && *response.Skip != observed) || (response.Limit != nil && *response.Limit != 100) {
			return fmt.Errorf("BloodHound returned inconsistent upload job pagination")
		}
		if response.Count != nil {
			if *response.Count < 0 || (expectedCount != nil && *expectedCount != *response.Count) {
				return fmt.Errorf("upload job count changed during validation")
			}
			expectedCount = response.Count
		}
		for _, job := range response.Data {
			// An ascending ID proves each job is new and catches ignored skip/sort
			// parameters without maintaining an extra set of all previously seen IDs.
			if job.ID <= previousID {
				return fmt.Errorf("upload job pagination repeated or reordered job %d", job.ID)
			}
			if err := validateCompletedJob(job); err != nil {
				return err
			}
			previousID = job.ID
			observed++
		}
		if expectedCount != nil && observed > *expectedCount {
			return fmt.Errorf("upload job count is smaller than the returned data")
		}
		if len(response.Data) < 100 || (expectedCount != nil && observed == *expectedCount) {
			if observed == 0 {
				return fmt.Errorf("at least one completed upload job is required")
			}
			if expectedCount != nil && observed != *expectedCount {
				return fmt.Errorf("upload job pagination ended before all jobs were returned")
			}
			return nil
		}
	}
	return fmt.Errorf("upload job pagination exceeded the dedicated-lab limit")
}
