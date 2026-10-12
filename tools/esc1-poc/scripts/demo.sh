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
mkdir -p work/demo

if [[ -z "${BH_TOKEN:-}" ]]; then
    echo "Set BH_TOKEN to a session token from the dedicated local BloodHound instance." >&2
    exit 1
fi

./esc1-poc schema
./esc1-poc run testdata/baseline/*.json | tee work/demo/baseline.json
batch_id=$(python3 -c 'import json; print(json.load(open("work/demo/baseline.json"))["batch_id"].removeprefix("upload-"))')

# The second invocation is a new process, checking saved graph state after restart.
# It uses the completed batch again; the default policy replaces the calculated edge.
./esc1-poc run -job-id "$batch_id" | tee work/demo/repeated.json
./esc1-poc run testdata/variants/two-templates/*.json | tee work/demo/two-templates.json
./esc1-poc run testdata/variants/one-support-remaining/*.json | tee work/demo/one-support.json
./esc1-poc run testdata/variants/all-supports-invalid/*.json | tee work/demo/no-support.json
./esc1-poc run testdata/baseline/*.json | tee work/demo/restored.json
./esc1-poc compare | tee work/demo/comparison.json

python3 - <<'PY'
import json
from pathlib import Path

def report(name):
    result = json.loads(Path(f"work/demo/{name}.json").read_text())
    assert result["committed"], result
    assert not result["native_comparison"]["missing"], result
    assert not result["native_comparison"]["extra"], result
    return result

expected = json.loads(Path("testdata/expected.json").read_text())
assert report("baseline")["desired_relationships"]["PoC_ADCSESC1"] == expected["baseline"]
repeated = report("repeated")
assert repeated["desired_relationships"]["PoC_ADCSESC1"] == expected["baseline"]
assert repeated["relationships_added"] == repeated["relationships_removed"] == repeated["desired_counts"]["PoC_ADCSESC1"], repeated
assert repeated["relationships_unchanged"] == 0, repeated
remaining = report("one-support")
assert remaining["relationships_unchanged"] == 0, remaining
assert remaining["relationships_added"] == remaining["relationships_removed"] == 1, remaining
assert remaining["desired_relationships"]["PoC_ADCSESC1"] == expected["one_support_remaining"]
removed = report("no-support")
assert removed["relationships_removed"] == 1, removed
assert removed["desired_relationships"]["PoC_ADCSESC1"] == expected["all_supports_invalid"]
assert report("restored")["relationships_added"] == 1
print("Native comparison, default replacement on rerun, restart, overlapping support, and removal checks passed.")
PY
