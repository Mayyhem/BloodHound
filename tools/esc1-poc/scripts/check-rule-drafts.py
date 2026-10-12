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
"""Check drafts in a disposable local service; never connect it to a graph database."""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.request

from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]


def extension_files(name, other):
    schema = {
        "schema": {"name": name, "display_name": name.title(), "namespace": "PoC_" + name, "version": "v1"},
        "node_kinds": [{"name": "PoC_" + name + "_Node"}],
        "relationship_kinds": [{"name": "PoC_" + name + "_Link", "compatible_kinds": [
            {"source_kind": "PoC_" + name + "_Node", "target_kind": "PoC_" + other + "_Node"}]}],
    }
    return {name + ".schema.json": json.dumps(schema),
        name + ".cypher": "// depends-on: none\nRETURN 'source' AS source, 'target' AS target, 'PoC_" + name + "_Link' AS kind"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--browser")
    parser.add_argument("--library-path")
    parser.add_argument("--output", type=Path, required=True)
    arguments = parser.parse_args()
    directory = arguments.output.resolve()
    directory.relative_to(ROOT / "work")
    directory.mkdir(parents=True, exist_ok=False)
    environment = dict(os.environ)
    environment["POSTPROCESSOR_RULE_TOKEN"] = secrets.token_hex(32)
    for name in ("BLOODHOUND_API_URL", "BLOODHOUND_API_TOKEN_ID", "BLOODHOUND_API_TOKEN_KEY"):
        environment.pop(name, None)
    if arguments.library_path:
        environment["LD_LIBRARY_PATH"] = arguments.library_path
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    results = {"checks": [], "browser_errors": []}
    process = None
    with sync_playwright() as playwright, (directory / "server.log").open("w") as log:
        browser = playwright.chromium.launch(executable_path=arguments.browser, env=environment)
        try:
            for first, other in [("first", "second"), ("second", "first")]:
                with socket.socket() as listener:
                    listener.bind(("127.0.0.1", 0))
                    port = listener.getsockname()[1]
                address = "http://127.0.0.1:" + str(port)

                def api(path, method="GET", body=None):
                    request = urllib.request.Request(address + "/v1/" + path, method=method,
                        headers={"Authorization": "Bearer " + environment["POSTPROCESSOR_RULE_TOKEN"], "Content-Type": "application/json"},
                        data=None if body is None else json.dumps(body).encode())
                    with client.open(request, timeout=10) as response:
                        return None if response.status == 204 else json.loads(response.read())

                def start():
                    nonlocal process
                    process = subprocess.Popen([str(ROOT / "esc1-poc"), "serve", "-listen", "127.0.0.1:" + str(port),
                        "-state", str(directory / first), "-local-ui-origin", address], env=environment, stdout=log, stderr=log)
                    deadline = time.monotonic() + 10
                    while time.monotonic() < deadline:
                        if process.poll() is not None:
                            raise RuntimeError("Temporary service exited; see server.log")
                        try:
                            api("draft")
                            return
                        except urllib.error.URLError:
                            time.sleep(0.05)
                    raise RuntimeError("Temporary service did not start")

                def stop():
                    nonlocal process
                    process.terminate()
                    process.wait(timeout=10)
                    process = None

                start()
                page = browser.new_page(viewport={"width": 1440, "height": 1100})
                page.on("pageerror", lambda error: results["browser_errors"].append(str(error)))

                def connect():
                    page.goto(address)
                    expect(page.locator("#message")).to_contain_text("up to date")
                    page.locator("#upload-controls > summary").click()

                def upload(files):
                    page.locator("#files").set_input_files([{"name": name, "mimeType": "text/plain", "buffer": contents.encode()} for name, contents in files.items()])
                    page.locator("#preview").click()

                connect()
                base = {path.name: path.read_text() for path in (ROOT / "examples/vault/extension").iterdir() if path.is_file()}
                upload(base)
                expect(page.locator("#review")).to_be_visible()
                page.locator("#activate").click()
                expect(page.locator("#message")).to_contain_text("activated")
                active = api("rules")
                upload(extension_files(first, other))
                expect(page.locator("#draft-state")).to_contain_text("cannot be activated")
                expect(page.locator("#draft-state")).to_contain_text("PoC_" + other + "_Node")
                expect(page.locator("#review-draft")).to_be_disabled()
                assert api("rules") == active
                saved = api("draft")
                page.screenshot(path=str(directory / (first + "-incomplete.png")), full_page=True)
                stop()
                start()
                connect()
                assert api("draft") == saved
                upload(extension_files(other, first))
                expect(page.locator("#review")).to_be_visible()
                assert api("rules") == active
                page.locator("#cancel-preview").click()
                assert api("draft")["ready"]
                page.locator("#review-draft").click()
                expect(page.locator("#review")).to_be_visible()
                page.screenshot(path=str(directory / (first + "-review.png")), full_page=True)
                page.locator("#activate").click()
                expect(page.locator("#message")).to_contain_text("activated")
                applied = api("rules")
                assert applied["files"] == {**base, **extension_files(first, other), **extension_files(other, first)}
                assert not api("draft")["revision"]
                name = first + ".cypher"
                upload({name: applied["files"][name] + "\n// first draft edit"})
                expect(page.locator("#overwrite-dialog")).to_be_visible()
                page.locator("#overwrite-file").click()
                expect(page.locator("#review")).to_be_visible()
                draft = api("draft")
                newer = api("draft", "PUT", {"base_revision": applied["revision"], "draft_revision": draft["revision"],
                    "files": {name: applied["files"][name] + "\n// newer draft edit"}, "overwrite": [name]})
                page.locator("#activate").click()
                expect(page.locator("#message")).to_contain_text("Refresh and review")
                assert api("rules") == applied
                page.locator("#refresh").click()
                expect(page.locator("#message")).to_contain_text("up to date")
                assert api("draft")["revision"] == newer["revision"]
                page.locator("#discard-draft").click()
                expect(page.locator("#message")).to_contain_text("Saved draft discarded")
                assert api("rules") == applied
                assert not api("draft")["revision"]
                results["checks"].append({"first_upload": first, "inactive_restart": True, "mutual_references": True,
                    "explicit_activation": True, "active_preserved": True, "stale_draft_rejected": True})
                page.close()
                stop()
            assert not results["browser_errors"], results["browser_errors"]
        finally:
            if process is not None:
                process.terminate()
                process.wait(timeout=10)
            browser.close()
    results["passed"] = True
    (directory / "checks.json").write_text(json.dumps(results, indent=2) + "\n")
    print("Draft browser checks passed for both upload orders, restart, activation, and stale review.")


if __name__ == "__main__":
    main()
