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

package datapipe

import "context"

// ExtensionAnalysis is optional work that runs after native analysis succeeds.
// Returning true means graph changes were committed and cached answers must go.
// This small interface keeps the normal import pipeline independent of the rule
// container's storage, HTTP requests, and execution implementation.
type ExtensionAnalysis interface {
	WithAnalysis(context.Context, func(context.Context) error) error
	AfterAnalysis(context.Context) (bool, error)
	Retry(context.Context) (bool, error)
}

func (s *BHCEPipeline) retryExtensions(ctx context.Context) error {
	if s.ExtensionAnalysis == nil {
		return nil
	}
	committed, err := s.ExtensionAnalysis.Retry(ctx)
	if committed {
		if resetError := s.cache.Reset(); resetError != nil {
			return resetError
		}
	}
	return err
}
