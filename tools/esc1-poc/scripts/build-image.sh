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
cd "$(dirname "$0")/../../.."
reference=2cb6dbd37738ffe31e34e1729b3638f3887a93e9

# Build the unchanged native application. New experiment files are separate;
# changes to tracked native files would invalidate our reference comparison.
if [[ "$(git rev-parse HEAD)" != "$reference" ]]; then
    echo "Use the recorded BloodHound reference commit before building its image." >&2
    exit 1
fi
if ! git diff --quiet "$reference" -- . ':!tools/esc1-poc'; then
    echo "Native BloodHound files have changes. The reference image must use unchanged native code." >&2
    exit 1
fi

# This uses the existing BloodHound Dockerfile, which may download its build
# dependencies. Run only after allowing those downloads and Docker storage.
docker compose --file tools/esc1-poc/compose.yaml build bloodhound
