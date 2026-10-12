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
"""Prepare synthetic inputs and check local OpenGraph demonstration evidence."""

import base64
import json
import os
import subprocess
import shutil
import sys
import urllib.request
import zipfile
from pathlib import Path


ALICE_SID = "S-1-5-21-111111111-222222222-333333333-1001"
VAULT_OWNER = "extension/vault_poc"
VAULT_RELATIONSHIP = "PoC_Vault_HasAccountFor"


def read_json(path):
    return json.loads(Path(path).read_text())


def write_json(path, contents):
    Path(path).write_text(json.dumps(contents, indent=2, sort_keys=True) + "\n")


def prepare(directory):
    directory = Path(directory)
    # Both archive forms use the same original synthetic bytes as the JSON runs.
    for filename, include_directory in [("vault-baseline.zip", False), ("mixed-baseline.zip", True)]:
        with zipfile.ZipFile(directory / filename, "w", zipfile.ZIP_DEFLATED) as archive:
            archive.write("examples/vault/collections/baseline.json", "vault.json")
            if include_directory:
                archive.write("testdata/baseline/users.json", "users.json")
    write_json(directory / "malformed-collection.json", {
        "graph": {"nodes": [], "edges": "This must be an array."},
        "metadata": {"source_kind": "PoC_Vault_Base"},
    })
    broken_extension = directory / "broken-extension"
    shutil.copytree("examples/vault/extension", broken_extension)
    helper_path = broken_extension / "vault_helpers.cypher"
    helper_query = helper_path.read_text()
    assert "' processing helper'" in helper_query
    helper_path.write_text(helper_query.replace("' processing helper'", "' staged rollback helper'"))
    final_path = broken_extension / "account_users.cypher"
    final_query = final_path.read_text()
    assert "UNWIND $vault_helpers AS helper" in final_query
    declarations = final_query.split("UNWIND $vault_helpers AS helper", 1)[0]
    final_path.write_text(declarations + "RETURN 1 / 0 AS source, 'unreachable' AS target\n")


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def postgres_snapshot():
    # Container-local psql uses the selected compose project and its native graph.
    # Build the same three row shapes as the Neo4j read-only snapshot.
    kinds = "(SELECT jsonb_agg(kind.name ORDER BY kind.name) FROM kind WHERE kind.id = ANY(node.kind_ids))"
    owned = "(properties->>'poc_owner' LIKE 'extension/vault_poc%' OR properties->>'poc_owner' LIKE 'extension/esc1_poc%')"
    statements = [
        f"SELECT jsonb_build_array(node.id, {kinds}, node.properties) FROM node WHERE {owned} ORDER BY node.id",
        "SELECT jsonb_build_array(edge.id, kind.name, source.properties->>'objectid', "
        "target.properties->>'objectid', edge.properties) FROM edge "
        "JOIN kind ON kind.id = edge.kind_id JOIN node source ON source.id = edge.start_id "
        "JOIN node target ON target.id = edge.end_id WHERE "
        "(edge.properties->>'poc_owner' LIKE 'extension/vault_poc%' OR edge.properties->>'poc_owner' LIKE 'extension/esc1_poc%') ORDER BY edge.id",
        f"SELECT jsonb_build_array(node.properties->>'objectid', {kinds}) FROM node "
        f"WHERE node.properties->>'objectid' = '{ALICE_SID}' "
        "OR EXISTS (SELECT 1 FROM kind WHERE kind.id = ANY(node.kind_ids) AND kind.name = 'PoC_Vault_Base') "
        "ORDER BY node.properties->>'objectid'",
    ]
    results = []
    for statement in statements:
        result = subprocess.run([
            "docker", "compose", "-f", "compose.pg.yaml", "exec", "-T", "pg-db",
            "psql", "-U", "bloodhound", "-d", "bloodhound", "-X", "-At",
            "-v", "ON_ERROR_STOP=1", "-c", statement,
        ], check=True, capture_output=True, text=True)
        results.append([json.loads(line) for line in result.stdout.splitlines() if line])
    return results


def neo4j_snapshot():
    # Read through the local graph API; never modify collected or native evidence.
    statements = [
        "MATCH (node) WHERE node.poc_owner STARTS WITH 'extension/vault_poc' "
        "OR node.poc_owner STARTS WITH 'extension/esc1_poc' "
        "RETURN id(node), labels(node), properties(node) ORDER BY id(node)",
        "MATCH (source)-[edge]->(target) WHERE edge.poc_owner STARTS WITH 'extension/vault_poc' "
        "OR edge.poc_owner STARTS WITH 'extension/esc1_poc' "
        "RETURN id(edge), type(edge), source.objectid, target.objectid, properties(edge) ORDER BY id(edge)",
        "MATCH (node) WHERE node.objectid = $alice OR node:PoC_Vault_Base "
        "RETURN node.objectid, labels(node) ORDER BY node.objectid",
    ]
    payload = {"statements": [{"statement": statement, "parameters": {"alice": ALICE_SID}} for statement in statements]}
    authorization = base64.b64encode(b"neo4j:experiment-only").decode()
    request = urllib.request.Request(
        "http://127.0.0.1:17474/db/neo4j/tx/commit",
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Basic " + authorization},
    )
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirects())
    with client.open(request, timeout=30) as response:
        result = json.load(response)
    assert not result.get("errors"), result
    assert len(result["results"]) == 3, result
    return [[entry["row"] for entry in rows["data"]] for rows in result["results"]]


def snapshot(path):
    driver = os.environ.get("GRAPH_DRIVER", "neo4j")
    if driver == "pg":
        nodes, relationships, collected_kinds = postgres_snapshot()
    elif driver == "neo4j":
        nodes, relationships, collected_kinds = neo4j_snapshot()
    else:
        raise ValueError("GRAPH_DRIVER must be neo4j or pg")
    for node in nodes:
        node[1].sort()
    for node in collected_kinds:
        node[1].sort()
        if node[0] == ALICE_SID:
            assert "User" in node[1] and "Base" in node[1], node
            assert not any(kind.startswith("PoC_Vault_") for kind in node[1]), node
        else:
            assert "User" not in node[1] and "Base" not in node[1], node
    write_json(path, {"nodes": nodes, "relationships": relationships, "collected_kinds": collected_kinds})


def verify(directory, scenario, order):
    directory = Path(directory)
    report = read_json(directory / (scenario + ".json"))
    graph = read_json(directory / (scenario + "-graph.json"))
    # Each tuple gives expected added, removed, unchanged, helper rows, helper changes, and contained account count.
    scenarios = {
        "first": (0, 0, 0, 1, 1, 1) if order == "vault-first" else (0, 0, 0, 0, 0, None),
        "second": (1, 0, 0, 1, 0 if order == "vault-first" else 1, 1),
        "repeated": (0, 0, 1, 1, 0, 1),
        "two-supports": (0, 0, 1, 1, 1, 2),
    }
    added, removed, unchanged, helper_rows, helper_changes, account_count = scenarios[scenario]
    empty = scenario == "first"
    expected = [] if empty else read_json("examples/vault/expected.json")["baseline"]
    assert report["committed"] and not report.get("stale_results", False), report
    assert report.get("native_comparison") is None, report
    assert (report["desired_relationships"][VAULT_RELATIONSHIP] or []) == expected, report
    assert report["relationships_added"] == added, report
    assert report["relationships_removed"] == removed, report
    assert report["relationships_unchanged"] == unchanged, report
    assert report["query_rows"]["vault_helpers.cypher"] == helper_rows, report
    assert report["query_rows"]["account_users.cypher"] == len(expected), report
    assert report["helpers_changed"] == helper_changes, report
    assert report["queries"] == ["vault_helpers.cypher", "account_users.cypher"], report
    helpers = [node for node in graph["nodes"] if node[2]["poc_owner"] == VAULT_OWNER + "/PoC_Vault_Helper"]
    assert len(helpers) == helper_rows, graph
    if helper_rows:
        helper = helpers[0]
        assert helper[1] == ["PoC_Vault_Helper"], helper
        assert helper[2]["objectid"] == "PoC_Vault_Helper_VAULT-DEMO", helper
        assert helper[2]["account_count"] == account_count, helper
        assert helper[2]["name"] == "VAULT-DEMO processing helper", helper
    edges = [edge for edge in graph["relationships"] if edge[1] == VAULT_RELATIONSHIP]
    actual = [{"source": edge[2], "target": edge[3]} for edge in edges]
    assert actual == expected, graph
    assert all(edge[4]["poc_owner"] == VAULT_OWNER + "/" + VAULT_RELATIONSHIP for edge in edges), graph


def compare(before_path, after_path, scope):
    before = read_json(before_path)
    after = read_json(after_path)
    if scope == "relationships":
        before, after = before["relationships"], after["relationships"]
    elif scope == "vault":
        def vault_objects(graph):
            return {
                "nodes": [node for node in graph["nodes"] if node[2]["poc_owner"].startswith(VAULT_OWNER)],
                "relationships": [edge for edge in graph["relationships"] if edge[4]["poc_owner"].startswith(VAULT_OWNER)],
                "collected_kinds": graph["collected_kinds"],
            }
        before, after = vault_objects(before), vault_objects(after)
    else:
        assert scope == "all", scope
    assert before == after, f"Graph evidence changed between {before_path} and {after_path} ({scope})."


def check_failure(path, failure_kind):
    report = read_json(path)
    assert not report["committed"] and report["stale_results"], report
    assert report.get("error") and "out of date" in report["error"].lower(), report
    if failure_kind == "malformed":
        assert "edges" in report["error"] and "array" in report["error"], report
        assert not report["batch_id"], report
    else:
        assert failure_kind == "broken", failure_kind
        assert "account_users.cypher" in report["error"], report
        assert report["queries"] == ["vault_helpers.cypher"], report
        assert report["helpers_changed"] == 1, report


def check_native(path):
    report = read_json(path)
    expected = read_json("testdata/expected.json")["baseline"]
    assert report["committed"] and not report.get("stale_results", False), report
    assert report["desired_relationships"]["PoC_ADCSESC1"] == expected, report
    assert report["native_comparison"]["missing_count"] == 0 and report["native_comparison"]["extra_count"] == 0, report


def summary(directory):
    directory = Path(directory)
    summary = {
        "passed": True,
        "orders": ["vault-first", "directory-first"],
        "checks": [
            "both ingestion orders", "OpenGraph ZIP and mixed OpenGraph/AD ZIP",
            "exact mixed-source pairs", "unchanged relationship identity",
            "two accounts collapse to one identity pair",
            "malformed upload preserves owned graph",
            "final query failure rolls back staged helper and successful-run marker",
            "native ESC1 comparison and separate ownership",
        ],
        "expected_pair": read_json("examples/vault/expected.json")["baseline"],
    }
    for order in summary["orders"]:
        order_directory = directory / order
        check_native(order_directory / "esc1.json")
        for failure_kind in ["malformed", "broken"]:
            check_failure(order_directory / (failure_kind + ".json"), failure_kind)
            compare(order_directory / (failure_kind + "-before.json"), order_directory / (failure_kind + "-after.json"), "all")
    write_json(directory / "summary.json", summary)


def main():
    action, *arguments = sys.argv[1:]
    if action == "prepare":
        prepare(*arguments)
    elif action == "snapshot":
        snapshot(*arguments)
    elif action == "verify":
        verify(*arguments)
    elif action == "compare":
        compare(*arguments)
    elif action == "failure":
        check_failure(*arguments)
    elif action == "native":
        check_native(*arguments)
    elif action == "summary":
        summary(*arguments)
    elif action == "job":
        batch_id = read_json(arguments[0])["batch_id"]
        assert batch_id.startswith("upload-") and batch_id.removeprefix("upload-").isdigit(), batch_id
        print(batch_id.removeprefix("upload-"))
    else:
        raise ValueError("Unknown demonstration action: " + action)


if __name__ == "__main__":
    main()
