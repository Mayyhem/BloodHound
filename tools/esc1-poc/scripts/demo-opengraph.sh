#!/usr/bin/env bash
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
set -euo pipefail
cd "$(dirname "$0")/.."

# Select the same native application and graph backend as the CLI.
export GRAPH_DRIVER="${GRAPH_DRIVER:-neo4j}"
case "$GRAPH_DRIVER" in
    neo4j) compose_file=compose.yaml ;;
    pg) compose_file=compose.pg.yaml ;;
    *) echo "GRAPH_DRIVER must be neo4j or pg." >&2; exit 1 ;;
esac

# Use fresh data for each order, and preserve it for inspection after shutdown.
if docker ps --filter label=com.docker.compose.project=esc1-poc \
    --format '{{.Label "com.docker.compose.service"}}' | \
    rg -q '^(bloodhound|graph-db|app-db|pg-db)$'; then
    echo "Stop the ordinary experiment stack before using its ports for this demonstration." >&2
    exit 1
fi
if [[ ! -x ./esc1-poc ]]; then
    echo "Build ./esc1-poc using scripts/go.sh before running this demonstration." >&2
    exit 1
fi
demo_stamp="$(date +%Y%m%d-%H%M%S)-$$"
demo_directory="$PWD/work/opengraph-$GRAPH_DRIVER-$demo_stamp"
mkdir -p "$demo_directory"
checker=scripts/check-opengraph-demo.py
vault_extension=examples/vault/extension
vault_collections=examples/vault/collections

# Install cleanup before startup so a partial startup is shut down too.
cleanup() {
    # Keep native application and database diagnostics before removing containers.
    docker compose -f "$compose_file" logs --no-color > "$POC_DATA_DIR/native-container.log" 2>&1 || true
    docker compose -f "$compose_file" down
}

for order in vault-first directory-first; do
    export COMPOSE_PROJECT_NAME="esc1-poc-opengraph-$order-$demo_stamp"
    export POC_DATA_DIR="$demo_directory/$order"
    export POC_PG_DATA_DIR="$POC_DATA_DIR"
    mkdir -p "$POC_DATA_DIR"
    python3 "$checker" prepare "$POC_DATA_DIR"
    trap cleanup EXIT
    docker compose -f "$compose_file" up -d
    export BH_TOKEN
    BH_TOKEN=$(python3 scripts/login.py)
    ./esc1-poc schema -extension "$vault_extension" > "$POC_DATA_DIR/vault-schema.json"
    ./esc1-poc schema > "$POC_DATA_DIR/esc1-schema.json"

    if [[ "$order" == vault-first ]]; then
        ./esc1-poc run -extension "$vault_extension" "$POC_DATA_DIR/vault-baseline.zip" > "$POC_DATA_DIR/first.json"
        python3 "$checker" snapshot "$POC_DATA_DIR/first-graph.json"
        python3 "$checker" verify "$POC_DATA_DIR" first "$order"
        ./esc1-poc run -extension "$vault_extension" testdata/baseline/*.json > "$POC_DATA_DIR/second.json"
    else
        ./esc1-poc run -extension "$vault_extension" testdata/baseline/*.json > "$POC_DATA_DIR/first.json"
        python3 "$checker" snapshot "$POC_DATA_DIR/first-graph.json"
        python3 "$checker" verify "$POC_DATA_DIR" first "$order"
        ./esc1-poc run -extension "$vault_extension" "$POC_DATA_DIR/mixed-baseline.zip" > "$POC_DATA_DIR/second.json"
    fi
    python3 "$checker" snapshot "$POC_DATA_DIR/second-graph.json"
    python3 "$checker" verify "$POC_DATA_DIR" second "$order"
    completed_job=$(python3 "$checker" job "$POC_DATA_DIR/second.json")

    ./esc1-poc run -extension "$vault_extension" -job-id "$completed_job" > "$POC_DATA_DIR/repeated.json"
    python3 "$checker" snapshot "$POC_DATA_DIR/repeated-graph.json"
    python3 "$checker" verify "$POC_DATA_DIR" repeated "$order"
    python3 "$checker" compare "$POC_DATA_DIR/second-graph.json" "$POC_DATA_DIR/repeated-graph.json" relationships

    ./esc1-poc run -extension "$vault_extension" "$vault_collections/two-supports.json" > "$POC_DATA_DIR/two-supports.json"
    python3 "$checker" snapshot "$POC_DATA_DIR/two-supports-graph.json"
    python3 "$checker" verify "$POC_DATA_DIR" two-supports "$order"
    python3 "$checker" compare "$POC_DATA_DIR/repeated-graph.json" "$POC_DATA_DIR/two-supports-graph.json" relationships
    completed_job=$(python3 "$checker" job "$POC_DATA_DIR/two-supports.json")

    # Both schemas remain installed. ESC1 uses its own native comparison and owner.
    ./esc1-poc run -job-id "$completed_job" > "$POC_DATA_DIR/esc1.json"
    python3 "$checker" snapshot "$POC_DATA_DIR/esc1-graph.json"
    python3 "$checker" native "$POC_DATA_DIR/esc1.json"
    python3 "$checker" compare "$POC_DATA_DIR/two-supports-graph.json" "$POC_DATA_DIR/esc1-graph.json" vault

    # Validation must fail before upload. The copied query fails after staging a helper.
    for failure in malformed broken; do
        python3 "$checker" snapshot "$POC_DATA_DIR/$failure-before.json"
        if [[ "$failure" == malformed ]]; then
            if ./esc1-poc run -extension "$vault_extension" "$POC_DATA_DIR/malformed-collection.json" > "$POC_DATA_DIR/$failure.json" 2> "$POC_DATA_DIR/$failure.stderr"; then
                echo "Malformed OpenGraph collection unexpectedly succeeded." >&2
                exit 1
            fi
        else
            if ./esc1-poc run -extension "$POC_DATA_DIR/broken-extension" -job-id "$completed_job" > "$POC_DATA_DIR/$failure.json" 2> "$POC_DATA_DIR/$failure.stderr"; then
                echo "Broken final query unexpectedly succeeded." >&2
                exit 1
            fi
        fi
        python3 "$checker" snapshot "$POC_DATA_DIR/$failure-after.json"
        python3 "$checker" failure "$POC_DATA_DIR/$failure.json" "$failure"
        python3 "$checker" compare "$POC_DATA_DIR/$failure-before.json" "$POC_DATA_DIR/$failure-after.json" all
    done

    cleanup
    trap - EXIT
done
python3 "$checker" summary "$demo_directory"
echo "Both fresh orders and publication checks passed. Reports: $demo_directory"
