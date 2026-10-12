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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/specterops/bloodhound/tools/esc1-poc/internal/ruleservice"
)

// The persistent service has no graph credentials and never controls Docker.
// It shares this executable so the existing small image can run either mode.
func serveRules(ctx context.Context, arguments []string) error {
	var (
		flags       = flag.NewFlagSet("serve", flag.ContinueOnError)
		address     = flags.String("listen", "127.0.0.1:18081", "HTTP address for rule uploads")
		directory   = flags.String("state", "work/rules", "persistent private rule directory")
		localOrigin = flags.String("local-ui-origin", "", "explicit loopback browser origin for local rule editing")
	)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("serve accepts flags, not collection files")
	}
	store, err := ruleservice.Open(*directory)
	if err != nil {
		return err
	}
	analysis, err := ruleservice.NewAnalysisClient(os.Getenv("BLOODHOUND_API_URL"), os.Getenv("BLOODHOUND_API_TOKEN_ID"), os.Getenv("BLOODHOUND_API_TOKEN_KEY"))
	if err != nil {
		return err
	}
	return ruleservice.Serve(ctx, *address, store, os.Getenv("POSTPROCESSOR_RULE_TOKEN"), *localOrigin, analysis)
}
