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
	"net/http"
	"testing"
)

func TestContainerOrigin(t *testing.T) {
	for _, origin := range []string{
		"http://172.18.0.3:8080", "https://10.0.0.2:8080/",
		"http://192.168.1.2", "https://[fd00::2]:8080",
	} {
		t.Run(origin, func(t *testing.T) {
			var client, err = NewContainer(origin, "token")
			if err != nil {
				t.Fatal(err)
			}
			if client.httpClient.Transport.(*http.Transport).Proxy != nil {
				t.Fatal("container credentials can use an environment proxy")
			}
			if err = client.httpClient.CheckRedirect(&http.Request{}, nil); err == nil {
				t.Fatal("container client allows redirects")
			}
			if _, err = New(origin, "token"); err == nil {
				t.Fatal("host client accepts a container origin")
			}
		})
	}
	for _, origin := range []string{
		"http://8.8.8.8:8080", "http://[2001:4860:4860::8888]:8080",
		"http://localhost:8080", "http://bloodhound.localhost:8080",
		"http://bloodhound:8080", "http://127.0.0.1:8080", "http://[::1]:8080",
		"http://169.254.1.2:8080", "http://0.0.0.0:8080",
		"ftp://172.18.0.3:8080", "http://user:pass@172.18.0.3:8080",
		"http://172.18.0.3:8080/path", "http://172.18.0.3:8080?other=1",
		"http://172.18.0.3:8080?", "http://172.18.0.3:8080#fragment",
		"http://172.18.0.3:invalid", "http:172.18.0.3:8080",
	} {
		t.Run(origin, func(t *testing.T) {
			if _, err := NewContainer(origin, "token"); err == nil {
				t.Fatal("accepted unsafe container origin")
			}
		})
	}
	for _, token := range []string{"", " ", "token\r\nother"} {
		if _, err := NewContainer("http://172.18.0.3:8080", token); err == nil {
			t.Fatal("accepted invalid token")
		}
	}
}
