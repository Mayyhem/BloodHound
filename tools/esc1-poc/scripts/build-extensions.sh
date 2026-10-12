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
experiment_root="$PWD/tools/esc1-poc"
build_directory="$experiment_root/work/extensions-build"
go_command="${POC_GO:-/snap/go/current/bin/go}"
umask 077
mkdir -p "$build_directory/native" "$build_directory/registry" "$experiment_root/work/go-cache" "$experiment_root/work/tmp" "$experiment_root/work/rules"
chmod 700 "$experiment_root/work/rules"

# Keep an operator's existing settings. A fresh token is written once, privately.
python3 - "$experiment_root" <<'PYSETTINGS'
import os, secrets, sys
from pathlib import Path
root = Path(sys.argv[1])
settings = root / 'rules.env'
if not settings.exists():
    text = (root / 'rules.env.example').read_text()
    text = text.replace('POSTPROCESSOR_RULE_TOKEN=\n', 'POSTPROCESSOR_RULE_TOKEN=' + secrets.token_hex(32) + '\n')
    text = text.replace('POSTPROCESSOR_UID=1000', 'POSTPROCESSOR_UID=' + str(os.getuid()))
    text = text.replace('POSTPROCESSOR_GID=1000', 'POSTPROCESSOR_GID=' + str(os.getgid()))
    with settings.open('x') as output:
        output.write(text)
os.chmod(settings, 0o600)
values = [line.split('=', 1)[1] for line in settings.read_text().splitlines() if line.startswith('POSTPROCESSOR_RULE_TOKEN=')]
if len(values) != 1 or len(values[0]) < 32:
    raise SystemExit('rules.env must contain one POSTPROCESSOR_RULE_TOKEN of at least 32 characters.')
PYSETTINGS

# UI assets are generated files. Add missing files only; preserve existing data.
# The supplied folder is the built UI root containing index.html and assets/.
ui_directory="${UI_ASSETS_DIR:-$experiment_root/work/ui-assets}"
python3 - "$ui_directory" "$PWD/cmd/api/src/api/static/assets" <<'PYASSETS'
import shutil, sys
from pathlib import Path
source, destination = map(Path, sys.argv[1:])
if source.is_dir():
    for item in source.rglob('*'):
        if item.is_symlink():
            raise SystemExit('UI assets must not contain symbolic links.')
        if not item.is_file():
            continue
        target = destination / item.relative_to(source)
        if target.exists():
            if not target.is_file() or target.read_bytes() != item.read_bytes():
                raise SystemExit('Refusing to replace an existing UI asset: ' + str(target))
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(item, target)
if not (destination / 'index.html').is_file():
    raise SystemExit('Built UI assets are required. Set UI_ASSETS_DIR to an existing UI build directory.')
PYASSETS

# Both builds use installed tooling and cached modules; no module downloads.
export GOCACHE="$experiment_root/work/go-cache"
export GOMODCACHE="${POC_MODULE_CACHE:-$("$go_command" env GOMODCACHE)}"
export GOTMPDIR="$experiment_root/work/tmp" TMPDIR="$experiment_root/work/tmp"
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOTELEMETRY=off GOWORK=off
export GOFLAGS=""
target_arch="${GOARCH:-$("$go_command" env GOHOSTARCH)}"
CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" "$go_command" build -p=2 -mod=readonly -trimpath -o "$build_directory/native/bloodhound" ./cmd/api/src/cmd/bhapi
(cd "$experiment_root"; CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" "$go_command" build -p=2 -mod=readonly -trimpath -o "$build_directory/registry/esc1-poc" .)
"$go_command" version -m "$build_directory/native/bloodhound" > "$build_directory/native-build-info.txt"
"$go_command" version -m "$build_directory/registry/esc1-poc" > "$build_directory/registry-build-info.txt"
sha256sum "$build_directory/native/bloodhound" "$build_directory/registry/esc1-poc" > "$build_directory/binary-sha256.txt"
cp "$experiment_root/Dockerfile.extensions" "$build_directory/native/Dockerfile"
cp "$experiment_root/Dockerfile" "$build_directory/registry/Dockerfile"
if [[ -z "${DOCKER_CONFIG:-}" && -f "$experiment_root/work/docker/config.json" ]]; then
    export DOCKER_CONFIG="$experiment_root/work/docker"
fi
# Each tiny Docker context contains its Dockerfile and one executable only.
docker build --network=none --pull=false -t "${EXTENSIONS_IMAGE:-esc1-poc-bloodhound-extensions:local}" "$build_directory/native"
docker build --network=none --pull=false -t "${POSTPROCESSOR_IMAGE:-esc1-poc-postprocessor:local}" "$build_directory/registry"
echo "Built separate extension-enabled BloodHound and rule registry images."
echo "Private settings: tools/esc1-poc/rules.env; build evidence: tools/esc1-poc/work/extensions-build"
