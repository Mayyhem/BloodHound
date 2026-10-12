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
base_image="esc1-poc-bloodhound:$reference"
base_id=sha256:f717a1d92bd7a44e04cee88c15df013e0143e303f514eaf8aeacce5494c71f1a
reference_image="esc1-poc-bloodhound-reference:$reference"
experiment_root="$PWD/tools/esc1-poc"
build_directory="$experiment_root/work/reference-build"
go_command="${POC_GO:-/snap/go/current/bin/go}"

# Build the recorded source, even while this working tree contains the optional
# native integration. The fresh directory prevents local edits entering the
# control image and does not require undoing anybody's work.
mkdir -p "$build_directory/context" "$experiment_root/work/go-cache" "$experiment_root/work/tmp"
reference_source=$(mktemp -d "$build_directory/source.XXXXXX")
trap 'rm -rf "$reference_source"' EXIT
git archive "$reference" | tar -x -C "$reference_source"
cp "$reference_source/go.mod" "$build_directory/native.mod"
cp "$reference_source/go.sum" "$build_directory/native.sum"
git rev-parse "$reference^{tree}" > "$build_directory/source-tree.txt"
cd "$reference_source"
export GOCACHE="$experiment_root/work/go-cache"
export GOMODCACHE="${POC_MODULE_CACHE:-$experiment_root/work/modules}"
export GOTMPDIR="$experiment_root/work/tmp"
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOTELEMETRY=off GOWORK=off
export GOFLAGS=""
target_arch="$("$go_command" env GOHOSTARCH)"
if [[ "$("$go_command" list -mod=readonly -modfile="$build_directory/native.mod" -m -f '{{.Version}} {{if .Replace}}replaced{{end}}' github.com/specterops/dawgs)" != "v0.9.1 " ]]; then
    echo "The native reference must use original DAWGS v0.9.1 without a replacement." >&2
    exit 1
fi
# The archive has no Git metadata. Record its source identity above instead of
# accidentally embedding metadata from the enclosing, edited working tree.
CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" "$go_command" build -p=2 -trimpath -buildvcs=false -mod=readonly -modfile="$build_directory/native.mod" -o "$build_directory/context/bloodhound" ./cmd/api/src/cmd/bhapi
"$go_command" version -m "$build_directory/context/bloodhound" > "$build_directory/binary-build.txt"
sha256sum "$build_directory/context/bloodhound" > "$build_directory/binary-sha256.txt"
binary_hash="$(cut -d ' ' -f 1 "$build_directory/binary-sha256.txt")"
cp "$experiment_root/Dockerfile.bloodhound-reference" "$build_directory/context/Dockerfile"
# A dedicated tiny context contains no lab data, credentials, or source tree.
if [[ -z "${DOCKER_CONFIG:-}" && -f "$experiment_root/work/docker/config.json" ]]; then
    export DOCKER_CONFIG="$experiment_root/work/docker"
fi
if [[ "$(docker image inspect "$base_image" --format '{{.Id}}')" != "$base_id" ]]; then
    echo "The historical base image does not match its recorded identity." >&2
    exit 1
fi
docker build --network=none --tag "$reference_image" --label "org.opencontainers.image.revision=$reference" --label "poc.native.binary.sha256=$binary_hash" "$build_directory/context"
docker image inspect "$reference_image" --format '{{.Id}}' > "$build_directory/image-id.txt"
echo "Built separate API reference image: $reference_image"
echo "Default development version: 999.999.999; original DAWGS: v0.9.1"
echo "Build metadata: $build_directory"
