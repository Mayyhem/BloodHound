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

# Each order gets fresh application and graph data. Reusing the first graph for
# the reverse order would not test whether arrival order changes the answer.
# Existing data is never erased. The ordinary stack must first be stopped.
if [[ -n "$(docker compose -f compose.yaml ps -q bloodhound)" ]]; then
    echo "Stop the ordinary experiment stack before using its ports for the order demonstration." >&2
    exit 1
fi
demo_directory="$PWD/work/order-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$demo_directory"

for order in user-first group-first; do
    export COMPOSE_PROJECT_NAME="esc1-poc-$order"
    export POC_DATA_DIR="$demo_directory/$order"
    mkdir -p "$POC_DATA_DIR"
    docker compose -f compose.yaml up -d
    trap 'docker compose -f compose.yaml down' EXIT
    export BH_TOKEN
    BH_TOKEN=$(python3 scripts/login.py)
    ./esc1-poc schema

    # Supply all other certificate and directory evidence first. Users and
    # groups then arrive in separate successful collection batches.
    ./esc1-poc run testdata/baseline/domains.json testdata/baseline/computers.json \
        testdata/baseline/certtemplates.json testdata/baseline/enterprisecas.json \
        testdata/baseline/rootcas.json testdata/baseline/ntauthstores.json > "$POC_DATA_DIR/initial.json"
    if [[ "$order" == user-first ]]; then
        ./esc1-poc run testdata/baseline/users.json > "$POC_DATA_DIR/first.json"
        ./esc1-poc run testdata/baseline/groups.json > "$POC_DATA_DIR/final.json"
    else
        ./esc1-poc run testdata/baseline/groups.json > "$POC_DATA_DIR/first.json"
        ./esc1-poc run testdata/baseline/users.json > "$POC_DATA_DIR/final.json"
    fi
    docker compose -f compose.yaml down
    trap - EXIT
done

python3 - "$demo_directory" <<'PY'
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
expected = json.loads(Path("testdata/expected.json").read_text())["baseline"]
for order in ["user-first", "group-first"]:
    report = json.loads((root / order / "final.json").read_text())
    assert report["committed"], report
    assert report["desired_relationships"]["PoC_ADCSESC1"] == expected, report
    assert report["native_comparison"]["missing_count"] == 0 and report["native_comparison"]["extra_count"] == 0, report
print(f"Both fresh collection orders produced the expected connections. Reports: {root}")
PY
