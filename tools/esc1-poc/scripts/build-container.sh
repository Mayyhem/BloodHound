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

# Create private local settings once. Never replace an operator's saved token
# or choice of containers. Matching the host user also keeps report files usable
# by both the host command and the container without changing their permissions.
if [[ ! -f postprocessor.env ]]; then
    umask 077
    socket_group=$(stat -c %g /var/run/docker.sock)
    sed -e "s/^POSTPROCESSOR_UID=.*/POSTPROCESSOR_UID=$(id -u)/" \
        -e "s/^POSTPROCESSOR_GID=.*/POSTPROCESSOR_GID=$(id -g)/" \
        -e "s/^DOCKER_SOCKET_GID=.*/DOCKER_SOCKET_GID=$socket_group/" \
        postprocessor.env.example > postprocessor.env
fi

# The existing Go wrapper uses installed tooling and disables module downloads.
target_arch="${GOARCH:-$(./scripts/go.sh env GOHOSTARCH)}"
mkdir -p work/container-build
CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" ./scripts/go.sh build -trimpath -o work/container-build/esc1-poc .

# Stage a tiny allowlisted context: never send lab data, tokens, or source to Docker.
cp Dockerfile .dockerignore work/container-build/
# The prepared lab has a local fix for its Docker plugin search path. Respect
# an explicit user setting, and use that existing fix when one is available.
if [[ -z "${DOCKER_CONFIG:-}" && -f "$PWD/work/docker/config.json" ]]; then
    export DOCKER_CONFIG="$PWD/work/docker"
fi
docker compose --env-file postprocessor.env --file compose.postprocessor.yaml build postprocessor
