"""Recreate the OpenGraph test files used in this investigation.

Usage: python3 make-test-files.py OUTPUT_FOLDER

Every file is an ordinary OpenGraph collection. The name of the "source kind" in each file's
metadata becomes the Neo4j label that BloodHound uses to find existing objects during import,
which is what this investigation is about. Each test uses a source kind that is not in the graph
yet, so no earlier upload can affect its timing.
"""

import json
import os
import sys


def collection(source_kind, node_count, edge_count=0, node_properties=None, edge_properties=None):
    """Build one collection: numbered objects, and optionally a chain of relationships between them."""
    nodes = []
    for number in range(node_count):
        properties = {"name": f"object {number}"}
        properties.update(node_properties or {})
        nodes.append({"id": f"{source_kind}-{number}", "kinds": ["PerfNode"], "properties": properties})

    edges = []
    for number in range(edge_count):
        edges.append(
            {
                "start": {"match_by": "id", "value": f"{source_kind}-{number}"},
                "end": {"match_by": "id", "value": f"{source_kind}-{number + 1}"},
                "kind": "PerfLink",
                "properties": dict(edge_properties or {}),
            }
        )

    return {"metadata": {"source_kind": source_kind}, "graph": {"nodes": nodes, "edges": edges}}


def write(folder, file_name, document):
    with open(os.path.join(folder, file_name), "w") as output:
        json.dump(document, output)


def main():
    folder = sys.argv[1]
    os.makedirs(folder, exist_ok=True)

    # The lab upload that exposed the problem: 100,000 objects and 50,000 relationships, with
    # padded properties so the file is about 56 MB. The original used source kind "TestBase",
    # object IDs "BG-<n>", object kind "TestNode", relationship kind "TestEdge", and one more
    # property per object, "lastlogon", a number. Those differences do not change what is measured.
    write(
        folder,
        "huge.json",
        collection(
            "HugeTest",
            100_000,
            50_000,
            node_properties={"description": "x" * 300, "enabled": True},
            edge_properties={"note": "y" * 100},
        ),
    )

    # Lab comparison through BloodHound: identical 20,000-object files. An ID index is created
    # by hand for PerfIdx before upload; PerfNoIdx has none.
    write(folder, "PerfIdx.json", collection("PerfIdx", 20_000))
    write(folder, "PerfNoIdx.json", collection("PerfNoIdx", 20_000))

    # Upstream comparison: PY20k gets a hand-made index; PX10k and PX20k have none, and PX20k
    # also has an extension schema installed first, to show a schema does not add an index.
    write(folder, "PY20k.json", collection("PY20k", 20_000))
    write(folder, "PX10k.json", collection("PX10k", 10_000))
    write(folder, "PX20k.json", collection("PX20k", 20_000))
    write(
        folder,
        "schema_PX20k.json",
        {
            "schema": {"name": "PX20k", "display_name": "Perf test", "version": "v0.0.1", "namespace": "PX20k"},
            "node_kinds": [{"name": "PX20k_Node", "display_name": "Perf node", "is_display_kind": True}],
            "relationship_kinds": [{"name": "PX20k_Link", "is_traversable": True}],
        },
    )


if __name__ == "__main__":
    main()
