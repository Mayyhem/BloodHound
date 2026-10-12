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
if [[ $# -ne 1 || ! -f "$1" ]]; then
    echo "Usage: scripts/demo-lab.sh /path/to/collection.zip" >&2
    exit 1
fi
collection_path=$(realpath "$1")
cd "$(dirname "$0")/.."

# A real lab gets its own data directory. Detailed reports can contain lab
# identifiers, so retain them only under the ignored work directory.
if [[ -n "$(docker compose -f compose.yaml ps -q bloodhound)" ]]; then
    echo "Stop the ordinary experiment stack before using its ports for the lab comparison." >&2
    exit 1
fi
export COMPOSE_PROJECT_NAME="esc1-poc-lab"
export POC_DATA_DIR="$PWD/work/lab-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$POC_DATA_DIR"
docker compose -f compose.yaml up -d
trap 'docker compose -f compose.yaml down' EXIT
export BH_TOKEN
BH_TOKEN=$(python3 scripts/login.py)
./esc1-poc schema
./esc1-poc run -timeout 30m "$collection_path" > "$POC_DATA_DIR/report.json"
batch_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["batch_id"].removeprefix("upload-"))' "$POC_DATA_DIR/report.json")
./esc1-poc run -timeout 30m -job-id "$batch_id" > "$POC_DATA_DIR/repeated.json"
./esc1-poc compare -timeout 30m > "$POC_DATA_DIR/comparison.json"
python3 - "$POC_DATA_DIR" <<'CHECK'
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
first = json.loads((root / "report.json").read_text())
second = json.loads((root / "repeated.json").read_text())
for report in [first, second]:
    assert report["committed"], "Lab run did not commit; inspect its local report."
    assert report["native_comparison"]["missing_count"] == 0 and report["native_comparison"]["extra_count"] == 0, "Native results differ."
desired_count = sum(second["desired_counts"].values())
assert second["relationships_added"] == second["relationships_removed"] == desired_count
assert second["relationships_unchanged"] == 0
assert first["desired_counts"] == second["desired_counts"]
assert first["desired_relationships"] == second["desired_relationships"]
print(f"Lab comparison matched {first['desired_counts']['PoC_ADCSESC1']} native pairs; the repeated run replaced the owned edges and kept the same desired pairs. Detailed reports: {root}")
CHECK
