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
"""Exercise the real rules page against disposable files and a fake schema reader.

This checks browser/file behavior, not native schema persistence or graph writes.
Those have separate Go/database tests. No existing service is contacted.
"""

import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import urllib.error
import urllib.request

from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]


def definition(number, namespace, version="v1.0.0"):
    return {"id": number, "name": namespace, "namespace": namespace, "version": version,
            "definition": {"schema": {"name": namespace, "namespace": namespace, "version": version},
                           "node_kinds": [], "relationship_kinds": [{"name": namespace + "_Link",
                               "compatible_kinds": [{"source_kind": "User", "target_kind": "Domain"}]}]}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--library-path")
    arguments = parser.parse_args()
    directory = arguments.output.resolve()
    directory.relative_to(ROOT / "work")
    directory.mkdir(parents=True, exist_ok=False)
    installed = []
    errors = []
    checks = []

    class Native(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            assert self.headers.get("Authorization", "").startswith("bhesignature ")
            data = {"/api/v2/extensions/definitions": {"definitions": installed},
                    "/api/v2/analysis/status": {"request_type": ""},
                    "/api/v2/datapipe/status": {"status": "idle"}}[self.path]
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"data": data}).encode())

    native = ThreadingHTTPServer(("127.0.0.1", 0), Native)
    threading.Thread(target=native.serve_forever, daemon=True).start()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    address = "http://127.0.0.1:" + str(port)
    environment = dict(os.environ)
    environment.update(POSTPROCESSOR_RULE_TOKEN=secrets.token_hex(32),
                       BLOODHOUND_API_URL="http://127.0.0.1:" + str(native.server_port),
                       BLOODHOUND_API_TOKEN_ID="12345678-1234-1234-1234-123456789abc",
                       BLOODHOUND_API_TOKEN_KEY=secrets.token_hex(32))
    if arguments.library_path:
        environment["LD_LIBRARY_PATH"] = arguments.library_path
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def api(name):
        request = urllib.request.Request(address + "/v1/" + name,
            headers={"Authorization": "Bearer " + environment["POSTPROCESSOR_RULE_TOKEN"]})
        with client.open(request, timeout=10) as response:
            return json.load(response)

    with (directory / "server.log").open("w") as log:
        process = subprocess.Popen([str(arguments.binary.resolve()), "serve", "-listen", "127.0.0.1:" + str(port),
            "-state", str(directory / "state"), "-local-ui-origin", address], env=environment, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 15
            while True:
                try:
                    api("draft")
                    break
                except urllib.error.URLError:
                    if process.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError("Temporary rules service did not start")
                    time.sleep(0.1)
            with sync_playwright() as playwright:
                browser = playwright.chromium.launch(env=environment)
                page = browser.new_page(viewport={"width": 1440, "height": 1100})
                page.on("pageerror", lambda error: errors.append(str(error)))
                page.route("**/*", lambda route: route.continue_() if route.request.url.startswith(address + "/") or route.request.url.startswith("blob:") else route.abort())
                page.goto(address, wait_until="networkidle")
                expect(page.locator("#message")).to_contain_text("up to date")
                assert page.locator("#files").get_attribute("accept") == ".cypher"

                def refresh():
                    page.locator("#refresh").click()
                    expect(page.locator("#message")).to_contain_text("up to date")

                def upload(namespaces):
                    if not page.locator("#files").is_visible():
                        page.locator("#upload-controls > summary").click()
                    payloads = []
                    for namespace in namespaces:
                        source = "// namespace: " + namespace + "\n// version: v1.0.0\n// extension-versions: >=v1.0.0,<v2.0.0\n// depends-on: none\nRETURN 'user' AS source, 'domain' AS target, '" + namespace + "_Link' AS kind"
                        payloads.append({"name": "users.cypher", "mimeType": "text/plain", "buffer": source.encode()})
                    page.locator("#files").set_input_files(payloads)
                    page.locator("#preview").click()

                upload(["PoC_AD"])
                expect(page.locator("#draft-state")).to_contain_text("cannot be activated")
                assert not api("draft")["ready"]
                installed.append(definition(1, "PoC_AD"))
                refresh()
                page.locator("#review-draft").click()
                page.locator("#activate").click()
                expect(page.locator("#message")).to_contain_text("activated")
                checks.append("An analysis uploaded before its schema stays a draft and activates only after review")

                installed.extend([definition(2, "PoC_Vault"), definition(3, "PoC_CyberArk")])
                upload(["PoC_Vault", "PoC_CyberArk"])
                expect(page.locator("#review")).to_be_visible()
                page.locator("#activate").click()
                expect(page.locator("#message")).to_contain_text("activated")
                expected_names = {name + "/users.cypher" for name in ["PoC_AD", "PoC_Vault", "PoC_CyberArk"]}
                assert set(api("rules")["files"]) == expected_names
                checks.append("Three independently associated files share the same original filename")

                installed[0] = definition(1, "PoC_AD", "v1.1.0")
                refresh()
                query = next(query for query in api("plan")["queries"] if query["namespace"] == "PoC_AD")
                assert query["enabled"] and query["version"] == "v1.0.0" and query["schema_version"] == "v1.1.0"
                installed.pop(0)
                refresh()
                assert set(api("rules")["files"]) == expected_names
                assert api("rules")["suspended"].keys() == {"PoC_AD/users.cypher"}
                installed.append(definition(4, "PoC_AD"))
                refresh()
                assert not next(query for query in api("plan")["queries"] if query["namespace"] == "PoC_AD")["enabled"]
                checks.append("Compatible schema upgrades require no file edits; deletion/reinstall retains a reviewed pause")
                page.screenshot(path=str(directory / "retained-files.png"), full_page=True)
                assert not errors, errors
                browser.close()
        finally:
            process.terminate()
            process.wait(timeout=10)
            native.shutdown()
            native.server_close()
    (directory / "results.json").write_text(json.dumps({"checks": checks, "browser_errors": errors}, indent=2) + "\n")
    print("Passed " + str(len(checks)) + " browser checks")


if __name__ == "__main__":
    main()
