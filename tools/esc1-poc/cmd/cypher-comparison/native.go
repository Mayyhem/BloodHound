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
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	adanalysis "github.com/specterops/bloodhound/packages/go/analysis/ad"
	"github.com/specterops/bloodhound/packages/go/analysis/post"
	"github.com/specterops/bloodhound/packages/go/graphschema/ad"
	"github.com/specterops/dawgs/graph"
	"github.com/specterops/dawgs/ops"
	"github.com/specterops/dawgs/query"
)

// Native calculation emits proposed edges. Spool those to a bounded temporary
// file, as the real native pipeline consumes a stream rather than retaining
// every duplicate job. Sorting, stable-ID lookup and hashing are CHECK time.
// This reference does not save native edges and is not a complete native run.
func measureNative(ctx context.Context, database graph.Database, result *measurement) error {
	var (
		started   = time.Now()
		cache     = adanalysis.NewADCSCache()
		jobs      = make(chan post.EnsureRelationshipJob)
		collected = make(chan error, 1)
	)
	// Retain the elapsed attempt even when preparation fails. Successful runs
	// set it before answer checking so that checking stays separately timed.
	defer func() {
		if result.Elapsed == 0 {
			result.Elapsed = time.Since(started)
		}
	}()
	directory, err := os.MkdirTemp("", "native-esc1-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	raw, err := os.Create(filepath.Join(directory, "jobs"))
	if err != nil {
		return err
	}
	defer raw.Close()
	localGroups, err := adanalysis.FetchLocalGroupData(ctx, database)
	if err != nil {
		return err
	}
	authorities, err := adanalysis.FetchNodesByKind(ctx, database, ad.EnterpriseCA)
	if err != nil {
		return err
	}
	templates, err := adanalysis.FetchNodesByKind(ctx, database, ad.CertTemplate)
	if err != nil {
		return err
	}
	if err := cache.BuildCache(ctx, database, authorities, templates); err != nil {
		return err
	}
	result.PreparationElapsed = time.Since(started)
	calculationStarted := time.Now()
	go func() {
		var (
			writer     = bufio.NewWriter(raw)
			bytes      int64
			writeError error
		)
		for job := range jobs {
			if writeError != nil {
				continue
			}
			written, err := fmt.Fprintf(writer, "%d %d\n", job.FromID, job.ToID)
			bytes += int64(written)
			if err != nil {
				writeError = err
			}
			// Allow room for the input, sorted output and sort's own merge
			// files. Later stages remove files they have finished reading.
			if bytes > result.Limits.DiskBytes/4 {
				writeError = fmt.Errorf("native checking files reached their temporary-space allowance")
			}
		}
		if err := writer.Flush(); writeError == nil {
			writeError = err
		}
		collected <- writeError
	}()
	err = database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
		for _, chains := range cache.GetECAHostedChainedDomains() {
			if err := adanalysis.PostADCSESC1(ctx, transaction, jobs, localGroups, chains, cache); err != nil {
				return err
			}
		}
		return nil
	})
	close(jobs)
	writeError := <-collected
	result.CalculationElapsed = time.Since(calculationStarted)
	result.Elapsed = time.Since(started)
	if err != nil {
		return err
	}
	if writeError != nil {
		return writeError
	}
	// The native job sender can stop on cancellation without returning an error.
	if err := ctx.Err(); err != nil {
		return err
	}
	checkStarted := time.Now()
	defer func() { result.CheckElapsed = time.Since(checkStarted) }()
	if err := raw.Close(); err != nil {
		return err
	}
	orderedJobs := filepath.Join(directory, "unique-jobs")
	sortFile := func(input, output string) error {
		command := exec.CommandContext(ctx, "sort", "-u", "-S", "8M", "-T", directory, "-o", output, input)
		command.Env = append(os.Environ(), "LC_ALL=C")
		return command.Run()
	}
	if err := sortFile(raw.Name(), orderedJobs); err != nil {
		return err
	}
	if err := os.Remove(raw.Name()); err != nil {
		return err
	}
	input, err := os.Open(orderedJobs)
	if err != nil {
		return err
	}
	defer input.Close()
	stable, err := os.Create(filepath.Join(directory, "stable-pairs"))
	if err != nil {
		return err
	}
	defer stable.Close()
	var (
		pending     [][2]graph.ID
		stableBytes int64
	)
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		var ids []graph.ID
		identifiers := map[graph.ID]string{}
		for _, pair := range pending {
			for _, id := range pair {
				if _, found := identifiers[id]; !found {
					identifiers[id] = ""
					ids = append(ids, id)
				}
			}
		}
		if err := database.ReadTransaction(ctx, func(transaction graph.Transaction) error {
			nodes, err := ops.FetchNodes(transaction.Nodes().Filter(query.In(query.NodeID(), ids)))
			if err != nil {
				return err
			}
			for _, node := range nodes {
				objectID, err := node.Properties.Get("objectid").String()
				if err != nil || objectID == "" {
					return fmt.Errorf("native answer has an invalid object identifier")
				}
				identifiers[node.ID] = objectID
			}
			return nil
		}); err != nil {
			return err
		}
		for _, pair := range pending {
			source, target := identifiers[pair[0]], identifiers[pair[1]]
			if source == "" || target == "" {
				return fmt.Errorf("native answer refers to an unresolved object")
			}
			encoded, err := json.Marshal([2]string{source, target})
			if err != nil {
				return err
			}
			stableBytes += int64(len(encoded) + 1)
			if stableBytes > result.Limits.DiskBytes/4 {
				return fmt.Errorf("native checking files reached their temporary-space allowance")
			}
			if _, err := stable.Write(append(encoded, '\n')); err != nil {
				return err
			}
			result.Connections++
		}
		pending = nil
		return nil
	}
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		var source, target int64
		if _, err := fmt.Sscanf(scanner.Text(), "%d %d", &source, &target); err != nil {
			return err
		}
		pending = append(pending, [2]graph.ID{graph.ID(source), graph.ID(target)})
		if len(pending) == 250 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	if err := input.Close(); err != nil {
		return err
	}
	if err := os.Remove(orderedJobs); err != nil {
		return err
	}
	if err := stable.Close(); err != nil {
		return err
	}
	orderedPairs := filepath.Join(directory, "ordered-pairs")
	if err := sortFile(stable.Name(), orderedPairs); err != nil {
		return err
	}
	pairs, err := os.Open(orderedPairs)
	if err != nil {
		return err
	}
	defer pairs.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, pairs); err != nil {
		return err
	}
	result.AnswerHash = hex.EncodeToString(digest.Sum(nil))
	return ctx.Err()
}
