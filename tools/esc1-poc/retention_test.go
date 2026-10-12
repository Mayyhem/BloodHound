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
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRunLogsRotateWithFiveFileLimitAndRejectOversizedRecords(t *testing.T) {
	var (
		directory = t.TempDir()
		payload   = strings.Repeat("x", 4*1024*1024)
	)
	// Each record fits, but two do not. Seven writes prove older generations
	// are discarded while the newest complete reports remain readable.
	for index := 0; index < 7; index++ {
		if err := appendRun(directory, runResult{Error: string(rune('a'+index)) + payload}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	logs := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "runs.jsonl") {
			continue
		}
		logs++
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if len(content) > 8*1024*1024 {
			t.Fatalf("log exceeded its allowance: %s", entry.Name())
		}
		var saved runResult
		if err := json.Unmarshal(content, &saved); err != nil {
			t.Fatalf("incomplete report: %s: %v", entry.Name(), err)
		}
		expected := byte('g')
		if entry.Name() != "runs.jsonl" {
			expected -= entry.Name()[len(entry.Name())-1] - '0'
		}
		if saved.Error[0] != expected {
			t.Fatalf("rotation order: %s starts with %q", entry.Name(), saved.Error[0])
		}
	}
	if logs != 5 {
		t.Fatalf("expected five logs, got %d", logs)
	}
	before, err := os.ReadFile(filepath.Join(directory, "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := appendRun(directory, runResult{Error: strings.Repeat("x", 8*1024*1024)}); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("oversized report accepted: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(directory, "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("oversized report changed saved evidence")
	}
}

func TestTemporaryCleanupRemovesOnlyOrphanCalculations(t *testing.T) {
	var (
		work      = t.TempDir()
		answers   = filepath.Join(work, "answers")
		preserved = []string{filepath.Join(work, "runs.jsonl"), filepath.Join(work, "explicit-report.json"), filepath.Join(answers, "report.json"), filepath.Join(answers, "calculation-report.json"), filepath.Join(answers, "reports", "saved.json")}
		orphan    = filepath.Join(answers, "calculation-interrupted", "partial.bin")
	)
	for _, name := range append(append([]string{}, preserved...), orphan) {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("preserve evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := lockWork(work)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	directory, err := prepareTemporaryDirectory(work)
	if err != nil {
		t.Fatal(err)
	}
	if directory != answers {
		t.Fatalf("unexpected answer directory: %s", directory)
	}
	if _, err := os.Stat(filepath.Dir(orphan)); !os.IsNotExist(err) {
		t.Fatalf("interrupted calculation remains: %v", err)
	}
	for _, name := range preserved {
		content, err := os.ReadFile(name)
		if err != nil || string(content) != "preserve evidence" {
			t.Fatalf("explicit evidence changed: %s: %v", name, err)
		}
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("answer directory not private: %v", info.Mode())
	}
}

func TestWorkAndReportLocksRejectConcurrentOwnersAndReleaseCleanly(t *testing.T) {
	var directory = t.TempDir()
	first, err := lockWork(directory)
	if err != nil {
		t.Fatal(err)
	}
	other, err := lockWork(directory)
	if err == nil {
		other.Close()
		first.Close()
		t.Fatal("second work owner acquired lock")
	}
	if !strings.Contains(err.Error(), "another command may be running") {
		t.Fatalf("unclear work lock error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := lockWork(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	// A command rejected by the work lock still needs to report its failure.
	// The separate report lock prevents its writer from racing log rotation.
	logLock, err := os.OpenFile(filepath.Join(directory, "runs.lock"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logLock.Close()
	if err := syscall.Flock(int(logLock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := appendRun(directory, runResult{Error: "rejected command"}); err == nil || !strings.Contains(err.Error(), "another process is saving a run report") {
		t.Fatalf("unclear report lock error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "runs.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("rejected writer created output: %v", err)
	}
	if err := syscall.Flock(int(logLock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := appendRun(directory, runResult{Error: "after release"}); err != nil {
		t.Fatal(err)
	}
}
