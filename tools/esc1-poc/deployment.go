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
	"context"
	"fmt"
	"os"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/bloodhound"
	"github.com/specterops/bloodhound/tools/esc1-poc/internal/graphdb"
)

// Both ways of running the program follow the same sequence. The difference is
// how they find the application, connect to its graph, and stop/restart it.
type deployment interface {
	verify(context.Context, string) error
	client() (*bloodhound.Client, error)
	database(context.Context) (graphdb.Connection, error)
	ready(context.Context) error
	withBloodHoundStopped(context.Context, func(context.Context) error) error
	images() map[string]string
}

func configuredDeployment(configuration configuration) (deployment, error) {
	switch os.Getenv("POSTPROCESSOR_MODE") {
	case "", "host":
		return stack{composePath: configuration.composePath, command: runDocker}, nil
	case "container":
		if (configuration.command == "run" || configuration.command == "compare") && os.Getenv(graphPasswordVariable()) == "" {
			return nil, fmt.Errorf("the selected graph password is required before uploading or stopping BloodHound")
		}
		return newContainerStack()
	default:
		return nil, fmt.Errorf("POSTPROCESSOR_MODE must be host or container")
	}
}

func (s stack) client() (*bloodhound.Client, error)                      { return localClient() }
func (s stack) database(ctx context.Context) (graphdb.Connection, error) { return localDatabase(ctx) }
func (s stack) ready(ctx context.Context) error                          { return waitForAPI(ctx) }
func (s stack) images() map[string]string                                { return nil }
