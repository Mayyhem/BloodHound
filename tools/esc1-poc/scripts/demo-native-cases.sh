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

# Use fresh data so a missing property really is missing, rather than a value
# left by an earlier upload. Keep the data and report for later inspection.
if [[ -n "$(docker compose -f compose.yaml ps -q bloodhound)" ]]; then
    echo "Stop the ordinary experiment stack before using its ports for these cases." >&2
    exit 1
fi
export COMPOSE_PROJECT_NAME="esc1-poc-native-cases"
export POC_DATA_DIR="$PWD/work/native-$(date +%Y%m%d-%H%M%S)"
python3 scripts/make-native-cases.py --output "$POC_DATA_DIR/collections"
docker compose -f compose.yaml up -d
trap 'docker compose -f compose.yaml down' EXIT
export BH_TOKEN
BH_TOKEN=$(python3 scripts/login.py)
./esc1-poc schema
./esc1-poc run \
    "$POC_DATA_DIR/collections/domains.json" \
    "$POC_DATA_DIR/collections/users.json" \
    "$POC_DATA_DIR/collections/groups.json" \
    "$POC_DATA_DIR/collections/computers.json" \
    "$POC_DATA_DIR/collections/certtemplates.json" \
    "$POC_DATA_DIR/collections/enterprisecas.json" \
    "$POC_DATA_DIR/collections/rootcas.json" \
    "$POC_DATA_DIR/collections/ntauthstores.json" > "$POC_DATA_DIR/report.json"

python3 - "$POC_DATA_DIR" <<'CHECK'
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
report = json.loads((root / "report.json").read_text())
expected = json.loads((root / "collections/expected.json").read_text())
expected.sort(key=lambda pair: (pair["source"], pair["target"]))
assert report["committed"], report
assert report["native_comparison"]["missing_count"] == 0 and report["native_comparison"]["extra_count"] == 0, report
assert report["desired_relationships"]["PoC_ADCSESC1"] == expected, report
print(f"All native cases matched their written expectations and BloodHound. Report: {root / 'report.json'}")
CHECK
