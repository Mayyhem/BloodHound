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

# Keep build products and temporary files beside the experiment. By default,
# Go cannot download anything. Preparing dependencies is a separate choice.
experiment_root="$PWD"
mkdir -p work/go-cache work/modules work/tmp
# Treat work as a separate module so "go test ./..." does not search database
# files, downloaded libraries, or temporary experiments for program source.
if [[ ! -f work/go.mod ]]; then
    printf 'module esc1-poc-work\n' > work/go.mod
fi
export GOCACHE="$experiment_root/work/go-cache"
export GOPATH="$experiment_root/work/go-path"
export GOMODCACHE="${POC_MODULE_CACHE:-$experiment_root/work/modules}"
export GOTMPDIR="$experiment_root/work/tmp"
export TMPDIR="$GOTMPDIR"
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
export GOTELEMETRY=off

# POC_GO can name another installed Go 1.27.1 executable. Calling the Snap
# binary directly avoids a launcher permission problem in the restricted shell.
if [[ -n "${POC_GO:-}" ]]; then
    go_command="$POC_GO"
elif [[ -x /snap/go/current/bin/go ]]; then
    go_command=/snap/go/current/bin/go
else
    go_command=go
fi
exec "$go_command" "$@"
