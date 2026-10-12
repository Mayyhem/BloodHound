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

package graphdb

import (
	"context"
	"testing"
)

func TestContainerAddressValidation(t *testing.T) {
	for _, address := range []string{
		"bolt://172.18.0.2:7687", "bolt+s://10.0.0.2:7687/",
		"bolt://192.168.1.2:7687", "bolt+s://[fd00::2]:7687",
	} {
		t.Run(address, func(t *testing.T) {
			if err := validateAddress(address, true); err != nil {
				t.Fatal(err)
			}
			if err := validateAddress(address, false); err == nil {
				t.Fatal("host connection accepts a container address")
			}
		})
	}
	for _, address := range []string{
		"bolt://8.8.8.8:7687", "bolt://[2001:4860:4860::8888]:7687",
		"bolt://localhost:7687", "bolt://neo4j:7687", "bolt://127.0.0.1:7687",
		"bolt://[::1]:7687", "bolt://169.254.1.2:7687", "bolt://0.0.0.0:7687",
		"neo4j://172.18.0.2:7687", "neo4j+s://172.18.0.2:7687",
		"bolt+ssc://172.18.0.2:7687", "http://172.18.0.2:7687",
		"bolt://user:pass@172.18.0.2:7687", "bolt://172.18.0.2:7687/path",
		"bolt://172.18.0.2:7687?other=1", "bolt://172.18.0.2:7687?",
		"bolt://172.18.0.2:7687#fragment", "bolt://172.18.0.2:invalid",
		"bolt:172.18.0.2:7687",
	} {
		t.Run(address, func(t *testing.T) {
			if err := validateAddress(address, true); err == nil {
				t.Fatal("accepted unsafe container address")
			}
			if _, err := OpenContainer(context.Background(), address, "neo4j", "password", "neo4j"); err == nil {
				t.Fatal("container connection bypassed validation")
			}
		})
	}
}

func TestHostAddressValidation(t *testing.T) {
	for _, address := range []string{"bolt://127.0.0.1:7687", "bolt+s://127.0.0.1:7687/"} {
		if err := validateAddress(address, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range []string{
		"bolt://localhost:7687", "neo4j://127.0.0.1:7687",
		"bolt://user:pass@127.0.0.1:7687", "bolt://127.0.0.1:7687/path",
		"bolt://127.0.0.1:7687?other=1", "bolt://127.0.0.1:7687#fragment",
	} {
		if _, err := Open(context.Background(), address, "neo4j", "password", "neo4j"); err == nil {
			t.Fatalf("host connection accepted %s", address)
		}
	}
}
