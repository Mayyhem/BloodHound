#!/usr/bin/env python3
# Copyright 2026 Specter Ops, Inc.
#
# Licensed under the Apache License, Version 2.0
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
"""Print a local development session token for capture in the BH_TOKEN variable."""

import json
import os
import sys
import time
import urllib.error
import urllib.request


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


# No proxies or redirects: the development password belongs only to this stack.
client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirects())
payload = json.dumps({
    "login_method": "secret",
    "username": os.environ.get("BH_USER", "admin"),
    "secret": os.environ.get("BH_PASSWORD", "experiment-only"),
}).encode()
deadline = time.monotonic() + 120
while True:
    request = urllib.request.Request(
        "http://127.0.0.1:18080/api/v2/login",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    try:
        with client.open(request, timeout=5) as response:
            result = json.load(response)
        token = result["data"]["session_token"]
        if not isinstance(token, str) or not token or "\n" in token:
            raise ValueError("login response has no valid session token")
        print(token)
        break
    except urllib.error.HTTPError as error:
        if error.code < 500:
            sys.exit(f"Local login failed with HTTP {error.code}; check the development account settings.")
    except (urllib.error.URLError, TimeoutError, ConnectionError):
        pass
    if time.monotonic() >= deadline:
        sys.exit("The dedicated local BloodHound login did not become ready within two minutes.")
    time.sleep(1)
