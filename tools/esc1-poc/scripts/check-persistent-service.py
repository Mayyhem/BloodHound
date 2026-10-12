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
"""Check the dedicated persistent-rule lab without restarting its services."""

import argparse
import copy
import datetime
import errno
import json
import os
import re
import shlex
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BLOODHOUND = "http://127.0.0.1:28080"
REGISTRY = "http://127.0.0.1:18081"
COLLECTIONS = ("users.json", "groups.json", "computers.json", "domains.json",
               "certtemplates.json", "enterprisecas.json", "rootcas.json", "ntauthstores.json")


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


class ResponseError(RuntimeError):
    def __init__(self, method, path, status, expected):
        super().__init__(f"{method} {path} returned HTTP {status}; expected {expected}")
        self.status = status


class Client:
    def __init__(self, base, token=""):
        self.base = base
        self.token = token
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirects())

    def request(self, method, path, payload=None, raw=None, filename=None, expected=200, timeout=15):
        headers = {}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        if payload is not None:
            raw = json.dumps(payload).encode()
        if raw is not None:
            headers["Content-Type"] = "application/json"
        if filename:
            headers["X-File-Upload-Name"] = filename
        request = urllib.request.Request(self.base + path, data=raw, method=method, headers=headers)
        try:
            response = self.opener.open(request, timeout=timeout)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            contents = response.read(8 << 20)
            status = response.status
        if status != expected:
            # Response bodies can contain sensitive login or collected values.
            raise ResponseError(method, path, status, expected)
        if not contents:
            return None
        if path == "/ui/":
            if b"<html" not in contents.lower():
                raise RuntimeError("The UI response is not an HTML document")
            return None
        return json.loads(contents)


def startup_login(client, seconds):
    """Wait for the local listener, then log in without hiding credential failures."""
    deadline = time.monotonic() + seconds
    healthy = False
    while time.monotonic() < deadline:
        try:
            remaining = max(0.01, min(15, deadline - time.monotonic()))
            if not healthy:
                client.request("GET", "/health", timeout=remaining)
                healthy = True
            return client.request("POST", "/api/v2/login", {
                "login_method": "secret", "username": "admin", "secret": "experiment-only",
            }, timeout=max(0.01, min(15, deadline - time.monotonic())))
        except ResponseError as error:
            if not 500 <= error.status < 600:
                raise
        except (ConnectionResetError, ConnectionRefusedError):
            pass
        except urllib.error.URLError as error:
            reason = error.reason
            if not isinstance(reason, OSError) or reason.errno not in (errno.ECONNRESET, errno.ECONNREFUSED):
                raise
        remaining = deadline - time.monotonic()
        if remaining > 0:
            time.sleep(min(1, remaining))
    raise RuntimeError("The dedicated BloodHound health check and login did not become ready before --timeout")


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    path.chmod(0o600)


def read_rule_token():
    for line in (ROOT / "rules.env").read_text().splitlines():
        if line.startswith("POSTPROCESSOR_RULE_TOKEN="):
            values = shlex.split(line.split("=", 1)[1], comments=True)
            if len(values) == 1 and len(values[0]) >= 32:
                return values[0]
    raise RuntimeError("rules.env has no valid registry token")


def timestamp(value):
    # Go reports up to nine fractional digits; Python 3.10 accepts six.
    normalized = re.sub(r"\.(\d+)(?=(?:Z|[+-]\d{2}:\d{2})$)",
                        lambda match: "." + (match.group(1) + "000000")[:6], value)
    return datetime.datetime.fromisoformat(normalized.replace("Z", "+00:00")).timestamp()


class ReadMonitor:
    """Keep status sampling separate from graph and UI reads so neither masks the other."""
    def __init__(self, bloodhound, registry, directory):
        self.bloodhound = bloodhound
        self.registry = registry
        self.directory = directory
        self.stop = threading.Event()
        self.samples = []
        self.statuses = []
        self.errors = []
        self.threads = []
        self.has_nodes = False

    def start(self):
        for action in (self.reads, self.rule_statuses):
            thread = threading.Thread(target=action, daemon=True)
            self.threads.append(thread)
            thread.start()

    def reads(self):
        while not self.stop.is_set():
            for name, method, path, payload in (
                ("graph", "POST", "/api/v2/graphs/cypher", {"query": "MATCH (n) RETURN n LIMIT 1"}),
                ("ui", "GET", "/ui/", None),
            ):
                if self.stop.is_set():
                    break
                sample = {"name": name, "started": time.time()}
                try:
                    self.bloodhound.request(method, path, payload)
                    sample["success"] = True
                    if name == "graph":
                        self.has_nodes = True
                except Exception as error:
                    # An empty graph legitimately returns 404 before the first import.
                    sample["error"] = str(error)
                    sample["empty_before_import"] = name == "graph" and not self.has_nodes and "HTTP 404" in str(error)
                    if not sample["empty_before_import"]:
                        self.errors.append(sample)
                sample["finished"] = time.time()
                self.samples.append(sample)
            self.stop.wait(0.1)

    def rule_statuses(self):
        previous = None
        while not self.stop.is_set():
            try:
                status = self.registry.request("GET", "/v1/status")
                identity = (status.get("event"), status.get("state"), status.get("updated_at"))
                if identity != previous:
                    self.statuses.append({"observed": time.time(), **status})
                    previous = identity
            except Exception as error:
                self.errors.append({"name": "registry", "error": str(error), "started": time.time()})
            self.stop.wait(0.02)

    def finish(self):
        self.stop.set()
        for thread in self.threads:
            thread.join(timeout=20)
        if any(thread.is_alive() for thread in self.threads):
            raise RuntimeError("A read probe did not finish")
        overlaps = {"graph": 0, "ui": 0}
        windows = []
        for index, status in enumerate(self.statuses):
            if status.get("state") != "running":
                continue
            end = next((candidate for candidate in self.statuses[index + 1:]
                        if candidate.get("event") == status.get("event") and candidate.get("state") != "running"), None)
            if end is None:
                continue
            start_time, end_time = timestamp(status["updated_at"]), timestamp(end["updated_at"])
            windows.append({"event": status["event"], "started": start_time, "finished": end_time})
            for sample in self.samples:
                if sample.get("success") and sample["started"] < end_time and sample["finished"] > start_time:
                    overlaps[sample["name"]] += 1
        summary = {"read_count": len(self.samples), "errors": self.errors, "running_windows": windows,
                   "reads_overlapping_rules": overlaps, "samples": self.samples, "registry_statuses": self.statuses}
        write_json(self.directory / "concurrent-reads.json", summary)
        return summary


def finish_monitor(monitor, result, directory):
    try:
        reads = monitor.finish()
        result["read_count"] = reads["read_count"]
        result["read_errors"] = len(reads["errors"])
        result["reads_overlapping_rules"] = reads["reads_overlapping_rules"]
        if reads["errors"] or not all(reads["reads_overlapping_rules"].values()):
            result["state"] = "failed"
            result.setdefault("error", "Concurrent reads failed or no graph/UI read overlapped a sampled running phase")
    except Exception as error:
        # Preserve the main failure even when probe evidence cannot be summarized.
        result["state"] = "failed"
        result["monitor_error"] = str(error)
        result.setdefault("error", "The concurrent-read monitor failed")
        write_json(directory / "concurrent-reads-unprocessed.json", {
            "samples": monitor.samples, "registry_statuses": monitor.statuses, "errors": monitor.errors,
        })
    write_json(directory / "summary.json", result)


def wait_native_idle(client, deadline):
    while time.monotonic() < deadline:
        pending = client.request("GET", "/api/v2/analysis/status")["data"]
        status = client.request("GET", "/api/v2/datapipe/status")["data"]
        if pending.get("request_type") not in ("", "analysis"):
            raise RuntimeError("Native analysis has a deletion or unknown queued request")
        if status.get("status") == "idle" and pending["request_type"] == "":
            started, completed = status.get("last_analysis_run_at"), status.get("last_complete_analysis_at")
            if not started or not completed or timestamp(completed) < timestamp(started) or timestamp(started) <= 0:
                raise RuntimeError("The latest native analysis has no successful completion")
            return status
        time.sleep(0.2)
    raise RuntimeError("Native analysis did not become successfully idle before the deadline")


def ingest(client, collections):
    return ingest_files(client, [collections / filename for filename in COLLECTIONS])


def ingest_files(client, paths):
    response = client.request("POST", "/api/v2/file-upload/start", expected=201)
    job_id = response["data"]["id"]
    for path in paths:
        client.request("POST", f"/api/v2/file-upload/{job_id}", raw=path.read_bytes(), filename=path.name, expected=202)
    client.request("POST", f"/api/v2/file-upload/{job_id}/end", expected=200)
    return job_id


def wait_execution(client, registry, revision, previous_event, job_id, desired_state, deadline, directory, file_count=8):
    while time.monotonic() < deadline:
        response = client.request("GET", f"/api/v2/file-upload?id=eq:{job_id}&limit=1")
        jobs = response["data"]
        if len(jobs) != 1 or jobs[0]["id"] != job_id:
            raise RuntimeError("The uploaded job was not returned unambiguously")
        job = jobs[0]
        status = registry.request("GET", "/v1/status")
        if job["status"] in (-1, 3, 4, 5, 8):
            write_json(directory / "native-job.json", job)
            raise RuntimeError("Native import or analysis failed")
        if status.get("event") != previous_event and status.get("revision") == revision and status.get("state") in ("success", "failed"):
            write_json(directory / "registry-status.json", status)
            if status["state"] != desired_state:
                raise RuntimeError(f"Rule execution reached {status['state']}; expected {desired_state}")
            if job["status"] == 2:
                write_json(directory / "native-job.json", job)
                if job.get("total_files") != file_count or job.get("failed_files") != 0 or job.get("partial_failed_files") != 0:
                    raise RuntimeError(f"Native job did not complete all {file_count} files without failures")
                native_status = wait_native_idle(client, deadline)
                write_json(directory / "native-status.json", native_status)
                return status
        time.sleep(0.2)
    raise RuntimeError("A fresh native job and matching rule execution did not finish before the deadline")


def edge_snapshot(client, kind, expected_empty=False):
    query = f"MATCH p=(s)-[r:{kind}]->(t) RETURN p"
    try:
        response = client.request("POST", "/api/v2/graphs/cypher", {"query": query, "include_properties": True})
    except ResponseError as error:
        if expected_empty and error.status == 404:
            return []
        raise
    data = response.get("data") if isinstance(response, dict) else None
    if not isinstance(data, dict) or data.get("literals") not in (None, []):
        raise RuntimeError("The graph API returned unexpected literals or no graph")
    nodes, edges = data.get("nodes"), data.get("edges")
    if not isinstance(nodes, dict) or not isinstance(edges, list):
        raise RuntimeError("The graph API returned an unexpected path shape")
    rows = []
    seen_ids = set()
    for edge in edges:
        if not isinstance(edge, dict) or edge.get("kind") != kind:
            raise RuntimeError("The graph API returned an unexpected relationship kind")
        edge_id = edge.get("id")
        if not isinstance(edge_id, str) or not edge_id.strip() or edge_id in seen_ids:
            raise RuntimeError("The graph API returned an empty, malformed, or duplicate edge ID")
        seen_ids.add(edge_id)
        properties = edge.get("properties")
        if not isinstance(properties, dict):
            raise RuntimeError("The graph API returned no persisted edge properties")
        endpoints = []
        for endpoint in ("source", "target"):
            graph_id = edge.get(endpoint)
            node = nodes.get(graph_id) if isinstance(graph_id, str) else None
            object_id = node.get("objectId") if isinstance(node, dict) else None
            if not isinstance(object_id, str) or not object_id.strip():
                raise RuntimeError("The graph API returned a missing or malformed edge endpoint")
            endpoints.append(object_id)
        # lastSeen is a display field; compare only persisted edge properties.
        rows.append({"source": endpoints[0], "target": endpoints[1], "id": edge_id, "properties": properties})
    return sorted(rows, key=lambda row: (row["source"], row["target"], row["id"]))


def assert_pairs(rows, expected):
    pairs = [(row["source"], row["target"]) for row in rows]
    if len(pairs) != len(expected) or set(pairs) != expected:
        raise RuntimeError(f"Exact expected pairs differ: received {len(pairs)}, expected {len(expected)}")


def activate_rules(registry, files):
    # This lab check intentionally replaces its rules; confirm the exact preview.
    try:
        active = registry.request("GET", "/v1/rules")
    except ResponseError as error:
        if error.status != 404:
            raise
        active = {"revision": "", "files": {}}
    change = {
        "base_revision": active["revision"],
        "files": files,
        "overwrite": sorted(name for name, contents in files.items()
                            if name in active["files"] and active["files"][name] != contents),
    }
    preview = registry.request("POST", "/v1/preview", change)
    return registry.request("PUT", "/v1/rules", {**change, "confirm_revision": preview["revision"]})


def execute_phase(name, files, desired_state, bloodhound, registry, args, expected, previous):
    directory = args.output / name
    directory.mkdir(mode=0o700)
    bundle = activate_rules(registry, files)
    if name != "baseline" and previous and bundle["revision"] == previous["revision"]:
        raise RuntimeError("The replacement bundle did not receive a new revision")
    job_id = ingest(bloodhound, args.collections)
    write_json(directory / "execution.json", {"revision": bundle["revision"], "job_id": job_id})
    status = wait_execution(bloodhound, registry, bundle["revision"], previous.get("event") if previous else None,
                            job_id, desired_state, time.monotonic() + args.timeout, directory)
    native = edge_snapshot(bloodhound, "ADCSESC1")
    experimental = edge_snapshot(bloodhound, "PoC_ADCSESC1")
    assert_pairs(native, expected)
    assert_pairs(experimental, expected)
    write_json(directory / "native-edges.json", native)
    write_json(directory / "experimental-edges.json", experimental)
    return status, experimental


def merge_vault_bundle(files):
    merged = copy.deepcopy(files)
    schema = json.loads(merged["schema.json"])
    vault_directory = ROOT / "examples" / "vault"
    vault_schema = json.loads((vault_directory / "extension" / "schema.json").read_text())
    for field in ("node_kinds", "relationship_kinds"):
        declared = {kind["name"]: kind for kind in schema[field]}
        for kind in vault_schema[field]:
            existing = declared.get(kind["name"])
            if existing is not None and existing != kind:
                raise RuntimeError(f"Conflicting {field} definition for {kind['name']}")
            if existing is None:
                schema[field].append(kind)
                declared[kind["name"]] = kind
    # This remains one bundle: vault kinds remain in the ESC1 schema for this historical demonstration.
    merged["schema.json"] = json.dumps(schema)
    for path in (vault_directory / "extension").glob("*.cypher"):
        contents = path.read_text()
        if path.name in merged and merged[path.name] != contents:
            raise RuntimeError(f"Conflicting query filename {path.name}")
        merged[path.name] = contents
    return merged


def check_opengraph(files, bloodhound, registry, args, expected, previous, result):
    bundle = activate_rules(registry, merge_vault_bundle(files))
    if bundle["revision"] == previous["revision"]:
        raise RuntimeError("The OpenGraph addition did not receive a new bundle revision")
    vault_directory = ROOT / "examples" / "vault" / "collections"
    users = ROOT / "testdata" / "baseline" / "users.json"
    user_records = json.loads(users.read_text())["data"]
    if len(user_records) != 1:
        raise RuntimeError("The public vault fixture must contain exactly one directory user")
    vault_pair = {("VAULT-DEMO", user_records[0]["ObjectIdentifier"])}
    # Separate uploads also prove that directory and OpenGraph evidence can
    # arrive in different jobs. Keep the exact completed-file count for each.
    previous_vault_edges = None
    for name, paths, vault_expected in (
        ("opengraph-directory", [users], None),
        ("opengraph-baseline", [vault_directory / "baseline.json"], vault_pair),
        ("opengraph-two-accounts", [vault_directory / "two-supports.json"], vault_pair),
        ("opengraph-repeat", [vault_directory / "two-supports.json"], vault_pair),
    ):
        directory = args.output / name
        directory.mkdir(mode=0o700)
        job_id = ingest_files(bloodhound, paths)
        write_json(directory / "execution.json", {"revision": bundle["revision"], "job_id": job_id, "file_count": len(paths)})
        previous = wait_execution(bloodhound, registry, bundle["revision"], previous["event"], job_id,
                                  "success", time.monotonic() + args.timeout, directory, file_count=len(paths))
        for kind, filename in (("ADCSESC1", "native-edges.json"), ("PoC_ADCSESC1", "experimental-edges.json")):
            edges = edge_snapshot(bloodhound, kind)
            assert_pairs(edges, expected)
            write_json(directory / filename, edges)
        # Before uploading the vault, it may be absent or left from an earlier
        # demonstration. Start its exact assertions with its first fresh upload.
        if vault_expected is not None:
            vault_edges = edge_snapshot(bloodhound, "PoC_Vault_HasAccountFor", expected_empty=not vault_expected)
            assert_pairs(vault_edges, vault_expected)
            if previous_vault_edges is not None and vault_edges != previous_vault_edges:
                raise RuntimeError("Vault keep-existing changed edge IDs or properties")
            previous_vault_edges = vault_edges
            write_json(directory / "vault-edges.json", vault_edges)
        result["phases"].append(name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--collections", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--opengraph", action="store_true", help="Also test vault imports and repeated identity associations; the active bundle retains both ESC1 and vault rules afterward")
    parser.add_argument("--timeout", type=float, default=900, help="Seconds allowed per import and analysis")
    args = parser.parse_args()
    args.collections = args.collections.resolve()
    args.output = args.output.resolve()
    if not args.output.is_relative_to((ROOT / "work").resolve()):
        parser.error("--output must be inside the PoC work directory")
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    os.umask(0o077)
    args.output.mkdir(parents=True, mode=0o700, exist_ok=False)
    expected_rows = json.loads((args.collections / "expected.json").read_text())
    expected = {(row["source"], row["target"]) for row in expected_rows}
    if len(expected) != 11 or len(expected_rows) != 11:
        raise RuntimeError("Supply the generated 18-case fixture with exactly 11 expected pairs")
    for filename in COLLECTIONS:
        if not (args.collections / filename).is_file():
            raise RuntimeError(f"Missing synthetic collection {filename}")
    bloodhound = Client(BLOODHOUND)
    login = startup_login(bloodhound, args.timeout)
    bloodhound.token = login["data"]["session_token"]
    if not isinstance(bloodhound.token, str) or not bloodhound.token or "\n" in bloodhound.token:
        raise RuntimeError("The local login returned no valid session token")
    registry = Client(REGISTRY, read_rule_token())
    wait_native_idle(bloodhound, time.monotonic() + args.timeout)
    files = {path.name: path.read_text() for path in (ROOT / "extension").iterdir() if path.name == "schema.json" or path.suffix == ".cypher"}
    monitor = ReadMonitor(bloodhound, registry, args.output)
    monitor.start()
    result = {"state": "failed", "phases": []}
    try:
        status, baseline = execute_phase("baseline", files, "success", bloodhound, registry, args, expected, registry.request("GET", "/v1/status"))
        result["phases"].append("baseline")
        keep_files = copy.deepcopy(files)
        keep_files["esc1.cypher"] += "\n// existing-edge-policy: keep-existing\n"
        status, kept = execute_phase("keep-existing", keep_files, "success", bloodhound, registry, args, expected, status)
        if kept != baseline:
            raise RuntimeError("keep-existing changed edge IDs or properties")
        result["phases"].append("keep-existing")
        active = registry.request("GET", "/v1/rules")
        rejected = registry.request("PUT", "/v1/rules", {
            "base_revision": active["revision"], "overwrite": ["esc1.cypher"],
            "files": {"esc1.cypher": keep_files["esc1.cypher"] + "\nCREATE (forbidden)\n"},
        }, expected=400)
        write_json(args.output / "rejected-write.json", rejected)
        if registry.request("GET", "/v1/rules") != active:
            raise RuntimeError("Rejected mutation changed the active bundle")
        result["phases"].append("write-query-rejected")
        replacement = copy.deepcopy(files)
        replacement["esc1.cypher"] += "\n// Persistent-service replacement check.\n"
        status, replaced = execute_phase("replace-existing", replacement, "success", bloodhound, registry, args, expected, status)
        if {row["id"] for row in replaced} & {row["id"] for row in kept}:
            raise RuntimeError("Default replacement retained an old experimental edge ID")
        result["phases"].append("replace-existing")
        broken = copy.deepcopy(replacement)
        broken["domain_helpers.cypher"] = broken["domain_helpers.cypher"].replace("domain.name", "persistentCheckMissingFunction(domain.name)")
        status, unchanged = execute_phase("runtime-failure", broken, "failed", bloodhound, registry, args, expected, status)
        if unchanged != replaced:
            raise RuntimeError("Failed rule execution changed prior edge IDs or properties")
        result["phases"].append("runtime-failure-retained-results")
        status, _ = execute_phase("recovered", replacement, "success", bloodhound, registry, args, expected, status)
        result["phases"].append("restored-valid-rules")
        if args.opengraph:
            check_opengraph(replacement, bloodhound, registry, args, expected, status, result)
        result["state"] = "success"
    except Exception as error:
        result["state"] = "failed"
        result["error"] = str(error)
    finally:
        finish_monitor(monitor, result, args.output)
    if result["state"] != "success":
        raise RuntimeError(result["error"])
    print(json.dumps({key: value for key, value in result.items() if key != "error"}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        raise SystemExit(str(error)) from None
