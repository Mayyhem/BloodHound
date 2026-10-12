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

# The build script stages only this Dockerfile and the static executable.
FROM scratch
COPY --chmod=0555 esc1-poc /postprocessor
WORKDIR /workspace
# Compose supplies the host user and the Docker socket's group. This fallback
# also avoids running as root when someone uses the image without Compose.
USER 65532:65532
ENTRYPOINT ["/postprocessor"]
