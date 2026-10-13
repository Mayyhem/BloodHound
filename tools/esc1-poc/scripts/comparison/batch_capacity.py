#!/usr/bin/env python3
# Copyright 2026 Specter Ops, Inc.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# SPDX-License-Identifier: Apache-2.0
"""Check batching's input-size limit using the completed million-object fixture.

This is a separate, deliberately wide-input rule, not an ESC1 capacity claim.
The complete input has 1,000 rows with 64 KiB of evidence per row. Both versions
have the unchanged 32 MiB parameter allowance; the batched version uses 500 rows.
"""
import argparse
import csv
import json
from pathlib import Path
import shutil

from run import Experiment, ROOT, WORK, file_sha256, variants


def execute(backend):
    previous = WORK / 'final-million' / backend
    if not json.loads((previous / 'complete.json').read_text())['complete']:
        raise RuntimeError('the final million-object study must finish first')
    arguments = argparse.Namespace(backend=backend, label='batch-capacity', objects=1000000,
                                   members=1000, authorities=2, shape='users', rounds=3,
                                   batch_rows=500, probes=False, replace=False,
                                   full_only=True, prepare_statistics=False)
    variants()
    experiment = Experiment(arguments)
    experiment.save('dataset.json', json.loads((previous / 'dataset.json').read_text()))
    schema = json.loads((ROOT / 'extension/schema.json').read_text())
    schema['schema']['display_name'] = 'Wide input comparison only'
    # The text is generated so the committed source need not contain 64 KiB of
    # repeated characters. The consumer checks all of it, so it is real input.
    # Select the intended users before returning the wide value. A prefix alone
    # also matches the unrelated users in this million-object graph; sorting
    # their wide rows before LIMIT can exhaust database resources during setup.
    producer = """// depends-on: node:User
MATCH (principal:User)
WHERE principal.objectid STARTS WITH 'Comparison_User_'
  AND principal.objectid >= 'Comparison_User_000000000'
  AND principal.objectid < 'Comparison_User_000001000'
RETURN principal.objectid AS source, '%s' AS evidence
ORDER BY source LIMIT 1000
""" % ('x' * 65536)
    consumer = """// depends-on: query:evidence.cypher, node:User, node:Domain
UNWIND $evidence AS row
WITH row WHERE row.evidence = '%s'
RETURN row.source AS source, 'Comparison_Domain' AS target,
       'PoC_ADCSESC1' AS kind, {evidence_checked:true} AS properties
""" % ('x' * 65536)
    for version in ('advanced', 'no-batch'):
        directory = experiment.directory / 'queries' / version
        shutil.rmtree(directory)
        directory.mkdir()
        (directory / 'schema.json').write_text(json.dumps(schema, indent=2) + '\n')
        (directory / 'evidence.cypher').write_text(producer)
        (directory / 'result.cypher').write_text(
            ('// batch-on: evidence.cypher\n' if version == 'advanced' else '') + consumer)
    experiment.save('capacity-settings.json', {
        'purpose': __doc__, 'source_fixture': 'final-million/' + backend,
        'input_rows': 1000, 'evidence_bytes_per_row': 65536,
        'query_sha256': {str(path.relative_to(experiment.directory / 'queries')): file_sha256(path)
                         for version in ('advanced', 'no-batch')
                         for path in sorted((experiment.directory / 'queries' / version).iterdir())},
    })
    experiment.run('restore-inputs', action='reset', measured=False)
    experiment.run('seed-previous-result', version='advanced', measured=False)
    for repeat in range(1, 4):
        order = ('advanced', 'no-batch') if repeat % 2 else ('no-batch', 'advanced')
        for version in order:
            if version == 'advanced':
                experiment.run('full', version=version, repeat=repeat)
                continue
            try:
                experiment.run('full', version=version, repeat=repeat)
            except RuntimeError:
                record = experiment.records[-1]
                if not record.get('exit_code') or 'parameter budget' not in (record.get('error') or ''):
                    raise
                if not record.get('previous_saved_state_preserved') or record['connections'] != 1000:
                    raise RuntimeError('failed whole-input run did not preserve its previous saved result')
            else:
                raise RuntimeError('whole input unexpectedly fit; investigate before reporting a capacity result')
    experiment.save('capacity-complete.json', {
        'complete': True,
        'note': 'Three matched repeats: batched completes; whole input fails the unchanged parameter limit and preserves the previous result. Failed elapsed times are not completion times or speed ratios.',
    })
    experiment.run('restore-inputs', action='reset', measured=False)
    return experiment


def publish(experiments):
    destination = ROOT / 'evidence/cypher-comparison'
    results = {}
    rows = []
    for experiment in experiments:
        records = []
        for item in experiment.records:
            if not item['measured']:
                continue
            record = dict(item)
            record['completed'] = record['exit_code'] == 0
            record['attempt_elapsed_ns'] = record.pop('engine_ns')
            record['completion_ns'] = record['attempt_elapsed_ns'] if record['completed'] else None
            records.append(record)
        results[experiment.arguments.backend] = {
            'settings': json.loads((experiment.directory / 'capacity-settings.json').read_text()),
            'build': json.loads((experiment.directory / 'settings.json').read_text())['binary_sha256'],
            'completion': json.loads((experiment.directory / 'capacity-complete.json').read_text()),
            'attempts': records,
        }
        rows.extend(records)
    (destination / 'batch-capacity.json').write_text(json.dumps(results, indent=2) + '\n')
    fields = sorted({key for row in rows for key in row if key != 'queries'})
    with (destination / 'batch-capacity.csv').open('w') as output:
        writer = csv.DictWriter(output, fieldnames=fields, extrasaction='ignore')
        writer.writeheader()
        writer.writerows(rows)


if __name__ == '__main__':
    publish([execute(backend) for backend in ('pg', 'neo4j')])
