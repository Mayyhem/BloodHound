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
"""Check the offline model viewer in a real browser, without downloading tools."""

import argparse
import copy
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import xml.etree.ElementTree as XML

from playwright.sync_api import sync_playwright


ROOT = Path(__file__).resolve().parents[1]


def read_json(path):
    return json.loads(path.read_text())


def triples(definition):
    return sorted(
        (pair["source_kind"], edge["name"], pair["target_kind"],
         "Traversable" if edge.get("is_traversable") is True else "Nontraversable",
         edge.get("existing_edge_policy", "Defined by the producing query"))
        for edge in definition["relationship_kinds"]
        for pair in edge["compatible_kinds"]
    )


def browser_triples(page):
    rows = page.locator("#relationship-table tbody tr").evaluate_all(
        "rows => rows.map(row => Array.from(row.cells, cell => cell.textContent))"
    )
    return sorted(tuple(row) for row in rows)


def check_full_model(page, definition):
    assert browser_triples(page) == triples(definition), f"The table changed the allowed pairs: {browser_triples(page)!r}, expected {triples(definition)!r}"
    names = {kind["name"] for kind in definition["node_kinds"]}
    external = {
        name
        for relationship in definition["relationship_kinds"]
        for pair in relationship["compatible_kinds"]
        for name in (pair["source_kind"], pair["target_kind"])
    } - names
    actual = set(page.locator("#nodes [data-kind]").evaluate_all(
        "nodes => nodes.map(node => node.dataset.kind)"
    ))
    assert actual == names | external, "A declared type, including an isolated type, is missing."
    external_actual = set(page.locator("#nodes .external").evaluate_all(
        "nodes => nodes.map(node => node.dataset.kind)"
    ))
    assert external_actual == external, "External types are not identified correctly."


def import_definition(page, definition, directory, filename):
    path = directory / filename
    path.write_text(json.dumps(definition, indent=2))
    page.locator("#schema-file").set_input_files(path)
    # File reading is asynchronous; wait for the file handler to update its title.
    page.wait_for_function(
        "title => document.querySelector('#model-title').textContent === title",
        arg=definition["schema"].get("display_name", definition["schema"]["name"]),
    )
    check_full_model(page, definition)


def drag(page, x, y, dx, dy):
    page.mouse.move(x, y)
    page.mouse.down()
    page.mouse.move(x + dx, y + dy, steps=8)
    page.mouse.up()


def check_controls(page, definition):
    for value, expected in [("true", "Traversable"), ("false", "Nontraversable")]:
        page.locator("#traversability").select_option(value)
        assert browser_triples(page) == [row for row in triples(definition) if row[3] == expected]
    page.locator("#traversability").select_option("all")
    relationship_name = definition["relationship_kinds"][0]["name"]
    page.locator("#search").fill(relationship_name)
    assert browser_triples(page) == [row for row in triples(definition) if relationship_name.lower() in " ".join(row[:3]).lower()]
    page.locator("#search").fill("")
    page.locator("#relationship-table tbody tr").first.click()
    assert relationship_name in page.locator("#details").inner_text()
    assert definition["relationship_kinds"][0].get("existing_edge_policy", "Defined by the producing query") in page.locator("#details").inner_text()
    node = page.locator("#nodes [data-kind]").first
    node.focus()
    node.press("Enter")
    assert node.get_attribute("data-kind") in page.locator("#details").inner_text()
    transform = page.locator("#viewport").get_attribute("transform")
    page.locator("#zoom-in").click()
    assert page.locator("#viewport").get_attribute("transform") != transform
    page.locator("#fit").click()
    transform = page.locator("#viewport").get_attribute("transform")
    graph_box = page.locator("#model-graph").bounding_box()
    drag(page, graph_box["x"] + 10, graph_box["y"] + 10, 35, 25)
    assert page.locator("#viewport").get_attribute("transform") != transform
    before = node.get_attribute("transform")
    circle_box = node.locator("circle").bounding_box()
    drag(page, circle_box["x"] + circle_box["width"] / 2,
         circle_box["y"] + circle_box["height"] / 2, 40, 20)
    assert page.locator("#nodes [data-kind]").first.get_attribute("transform") != before
    page.locator("#fit").click()
    check_full_model(page, definition)


def check_svg_download(page, directory, filename):
    with page.expect_download() as waiting:
        page.locator("#download-svg").click()
    path = directory / filename
    waiting.value.save_as(path)
    exported = XML.fromstring(path.read_text())
    nodes = {element.attrib["data-kind"] for element in exported.iter() if "data-kind" in element.attrib}
    expected_nodes = set(page.locator("#nodes [data-kind]").evaluate_all(
        "nodes => nodes.map(node => node.dataset.kind)"
    ))
    arrows = [element for element in exported.iter() if "marker-end" in element.attrib]
    assert nodes == expected_nodes, "SVG export omitted a visible type."
    assert len(arrows) == page.locator("#edges .edge-path").count(), "SVG export omitted an arrow."
    assert any(element.tag.endswith("style") for element in exported.iter()), "SVG styles are missing."
    return len(nodes), len(arrows)


def synthetic_definition():
    malicious = '</script><img src="https://example.invalid/should-not-load" onerror="window.viewerInjection=true">'
    return {
        "schema": {"name": "viewer_checks", "display_name": "Viewer checks " + malicious,
                   "version": "v1.0.0", "namespace": "PoC"},
        "node_kinds": [
            {"name": "PoC_Account", "display_name": malicious, "description": malicious,
             "color": 'url(https://example.invalid/color)'},
            {"name": "PoC_Isolated"},
        ],
        "relationship_kinds": [
            {"name": "PoC_First", "description": malicious, "is_traversable": True,
             "compatible_kinds": [{"source_kind": "PoC_Account", "target_kind": "User"}]},
            {"name": "PoC_Second", "is_traversable": True,
             "compatible_kinds": [{"source_kind": "PoC_Account", "target_kind": "User"}]},
            {"name": "PoC_Reverse", "is_traversable": False,
             "compatible_kinds": [{"source_kind": "User", "target_kind": "PoC_Account"}]},
            {"name": "PoC_Self", "is_traversable": False,
             "compatible_kinds": [{"source_kind": "PoC_Account", "target_kind": "PoC_Account"}]},
        ],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--browser", help="Use this installed Chromium executable.")
    parser.add_argument("--library-path", help="Extra existing library directories for Chromium.")
    parser.add_argument("--output", type=Path, default=ROOT / "work" / "model-viewer")
    arguments = parser.parse_args()
    directory = arguments.output.resolve()
    directory.relative_to(ROOT / "work")
    directory.mkdir(parents=True, exist_ok=True)
    results = {
        "checked_at_utc": datetime.now(timezone.utc).isoformat(),
        "checks": [],
        "network_requests": [],
        "browser_errors": [],
        "tested_controls": ["search", "traversability filters", "relationship details",
                            "type details", "zoom", "fit", "pan", "drag types",
                            "import resets filters", "invalid import retains model"],
        "snapshot_sha256": {
            str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in [ROOT / "model.html", ROOT / "examples/vault/model.html"]
        },
    }
    environment = dict(os.environ)
    if arguments.library_path:
        environment["LD_LIBRARY_PATH"] = arguments.library_path
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(executable_path=arguments.browser, env=environment)
        context = browser.new_context(viewport={"width": 1600, "height": 1100}, accept_downloads=True)
        # Reject any attempted web request. A standalone file needs no servers.
        def reject_network(route):
            results["network_requests"].append(route.request.url)
            route.abort()
        context.route("http://**/*", reject_network)
        context.route("https://**/*", reject_network)
        page = context.new_page()
        page.on("pageerror", lambda error: results["browser_errors"].append(str(error)))
        for name, schema_path, html_path in [
            ("esc1", ROOT / "extension/schema.json", ROOT / "model.html"),
            ("vault", ROOT / "examples/vault/extension/schema.json", ROOT / "examples/vault/model.html"),
        ]:
            definition = read_json(schema_path)
            page.goto(html_path.as_uri())
            page.wait_for_selector("#nodes [data-kind]")
            check_full_model(page, definition)
            check_controls(page, definition)
            node_count, arrow_count = check_svg_download(page, directory, name + ".svg")
            page.screenshot(path=str(directory / (name + ".png")), full_page=True)
            results["checks"].append({"model": name, "triples": len(triples(definition)), "nodes": node_count, "arrows": arrow_count})
        fixture = synthetic_definition()
        page.locator("#search").fill("previous filter")
        page.locator("#traversability").select_option("true")
        import_definition(page, fixture, directory, "synthetic.json")
        assert page.locator("#search").input_value() == ""
        assert page.locator("#traversability").input_value() == "all"
        assert page.locator('#edges [data-count="2"]').count() == 1, "Parallel relationships were lost."
        assert page.locator('#edges [data-source="PoC_Account"][data-target="PoC_Account"]').count() == 1
        check_controls(page, fixture)
        assert page.evaluate("window.viewerInjection === undefined")
        assert page.locator("img").count() == 0, "A label was interpreted as HTML."
        invalid = copy.deepcopy(fixture)
        invalid["relationship_kinds"][0]["compatible_kinds"] = []
        invalid_path = directory / "invalid.json"
        invalid_path.write_text(json.dumps(invalid))
        page.locator("#schema-file").set_input_files(invalid_path)
        page.wait_for_function("document.querySelector('#error').textContent.includes('Unable to load')")
        check_full_model(page, fixture)
        for index, value in enumerate([None, "", False, 12, [], {}, "unknown"]):
            invalid_policy = copy.deepcopy(fixture)
            invalid_policy["relationship_kinds"][0]["existing_edge_policy"] = value
            policy_path = directory / f"invalid-policy-{index}.json"
            policy_path.write_text(json.dumps(invalid_policy))
            page.locator("#error").evaluate("element => element.textContent = ''")
            page.locator("#schema-file").set_input_files(policy_path)
            page.wait_for_function("document.querySelector('#error').textContent.includes('existing_edge_policy')")
            check_full_model(page, fixture)
        check_svg_download(page, directory, "synthetic.svg")
        page.screenshot(path=str(directory / "synthetic.png"), full_page=True)
        # Also check malicious text in the page's initially embedded JSON.
        fixture_directory = directory / "embedded"
        fixture_directory.mkdir(exist_ok=True)
        (fixture_directory / "schema.json").write_text(json.dumps(fixture))
        html = subprocess.run([str(ROOT / "esc1-poc"), "model", "-extension", str(fixture_directory)], check=True, capture_output=True).stdout
        embedded_page = fixture_directory / "model.html"
        embedded_page.write_bytes(html)
        page.goto(embedded_page.as_uri())
        page.wait_for_selector("#nodes [data-kind]")
        check_full_model(page, fixture)
        assert page.evaluate("window.viewerInjection === undefined")
        assert page.locator("img").count() == 0
        results["checks"].append({"model": "synthetic", "triples": 4, "parallel_relationships": 2, "self_loops": 1, "safe_import_and_embedding": True, "invalid_import_keeps_previous_model": True})
        # Both schemas must be available before either reference is resolved.
        consumer = copy.deepcopy(fixture)
        consumer["relationship_kinds"].append({"name": "PoC_Cross", "compatible_kinds": [
            {"source_kind": "PoC_Account", "target_kind": "PoC_Other_Node"}]})
        supplier = {"schema": {"name": "other", "display_name": "Other", "namespace": "PoC_Other", "version": "v1"},
                    "node_kinds": [{"name": "PoC_Other_Node"}],
                    "relationship_kinds": [{"name": "PoC_Other_Back", "compatible_kinds": [
                        {"source_kind": "PoC_Other_Node", "target_kind": "PoC_Account"}]}]}
        consumer_path, supplier_path = directory / "consumer.schema.json", directory / "supplier.schema.json"
        consumer_path.write_text(json.dumps(consumer))
        supplier_path.write_text(json.dumps(supplier))
        combined = {"node_kinds": consumer["node_kinds"] + supplier["node_kinds"],
                    "relationship_kinds": consumer["relationship_kinds"] + supplier["relationship_kinds"]}
        for paths in [[consumer_path, supplier_path], [supplier_path, consumer_path]]:
            page.locator("#schema-file").set_input_files(paths)
            expected_title = " + ".join(read_json(path)["schema"]["display_name"] for path in paths)
            page.wait_for_function("title => document.querySelector('#model-title').textContent === title", arg=expected_title)
            check_full_model(page, combined)
        results["checks"].append({"model": "mutual-schema-references", "both_file_orders": True})
        assert not results["network_requests"], "The viewer attempted a network request."
        assert not results["browser_errors"], "The browser reported a script error."
        context.close()
        browser.close()
    results["passed"] = True
    (directory / "checks.json").write_text(json.dumps(results, indent=2) + "\n")
    print(json.dumps(results, indent=2))


if __name__ == "__main__":
    main()
