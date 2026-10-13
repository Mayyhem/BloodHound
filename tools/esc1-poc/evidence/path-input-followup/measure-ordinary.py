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

import hashlib,json,os,pathlib,subprocess,sys
root=pathlib.Path(sys.argv[1]).resolve()
os.chdir(pathlib.Path(__file__).resolve().parents[2])
if (root/"measurements.json").exists():
 raise SystemExit("Use a fresh output directory containing the two measurement binaries.")
records=[]
for backend,address in [('pg','postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison'),('neo4j','neo4j://neo4j:experiment-only@127.0.0.1:27687/neo4j')]:
 env=dict(os.environ,CONNECTION_STRING=address)
 for repetition in range(4):
  for variant in (['before','after'] if repetition%2==0 else ['after','before']):
   path=root/f'{backend}-{variant}-{repetition}.json'
   with path.open('w') as output, (root/'measurement.log').open('a') as log:
    subprocess.run([str(root/variant),'-action','run','-extension','examples/esc1-cypher','-run-timeout','5m'],env=env,stdout=output,stderr=log,check=True,timeout=360)
   data=json.loads(path.read_text())
   assert data['connections']==100, data['connections']
   records.append(dict(variant=variant,repetition=repetition,warmup=repetition==0,**data))
   print(backend,variant,repetition,'preparation',data['report']['preparation_nanoseconds']/1e9,'total',data['report']['elapsed_nanoseconds']/1e9,flush=True)
 hashes={r['saved_outputs_sha256'] for r in records if r['backend']==backend}
 assert len(hashes)==1, hashes
assert len({r['saved_outputs_sha256'] for r in records}) == 1
(root/'measurements.json').write_text(json.dumps(records,indent=2)+'\n')
(root/'binaries.json').write_text(json.dumps({v:hashlib.sha256((root/v).read_bytes()).hexdigest() for v in ['before','after']},indent=2)+'\n')
