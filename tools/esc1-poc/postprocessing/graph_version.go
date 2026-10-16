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
	"crypto/rand"
	"fmt"
)

// GraphVersion identifies one observed state of this application's graph.
// It is comparable so saved work can require an exact later match. The private
// random epoch changes on restart, while Revision advances for local writes.
//
// This is deliberately limited to writes admitted by this one BloodHound
// process. It cannot prove that another application or a direct database writer
// left the graph unchanged.
type GraphVersion struct {
	epoch    [16]byte
	Revision uint64
}

func newGraphVersion() GraphVersion {
	var version GraphVersion
	if _, err := rand.Read(version.epoch[:]); err != nil {
		panic(fmt.Sprintf("create graph version epoch: %v", err))
	}
	return version
}
