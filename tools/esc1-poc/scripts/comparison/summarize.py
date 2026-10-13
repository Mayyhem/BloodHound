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
"""Publish measurements from explicitly selected, completed synthetic studies.

Database copies and raw logs stay private. This exports measurements, settings,
and answer fingerprints, so a reviewer can reproduce each table without them.
It deliberately refuses incomplete studies rather than hiding failed runs.
"""
import argparse
from collections import defaultdict
import csv
import hashlib
import json
import math
from pathlib import Path
import statistics

ROOT = Path(__file__).resolve().parents[2]
WORK = ROOT / 'work/cypher-comparison'


def read(path):
    return json.loads(path.read_text())

def source_hash(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def middle_and_range(values):
    values = [value for value in values if value is not None]
    if not values:
        return None
    return {'count': len(values), 'median': statistics.median(values),
            'minimum': min(values), 'maximum': max(values)}


def latency(values):
    if not values:
        return None
    ordered = sorted(values)
    return {'count': len(values), 'median_ns': statistics.median(values),
            'p95_ns': ordered[math.ceil(len(ordered) * .95) - 1],
            'maximum_ns': ordered[-1], 'at_least_100_observations': len(values) >= 100}


def difference(value, reference):
    return {'reference': reference, 'comparison': value,
            'difference': value - reference,
            'percent': (value - reference) / reference * 100 if reference else None}


def review_targets(summaries):
    """Apply the proposed targets without treating them as user acceptance.

    Keep absolute differences alongside percentages. No near-zero allowance
    was agreed, and a sampled peak is not proof of the true maximum.
    """
    lookup = {(item['study'], item['version'], item['workload']): item for item in summaries}
    results = []
    for item in summaries:
        if item['version'] not in ('advanced', 'ordinary-all'):
            continue
        baseline = lookup[(item['study'], 'starting', item['workload'])]
        checks = {}
        for name, statistic, allowance in (
                ('engine_ns', 'median', .10), ('calculation_ns', 'median', .10),
                ('app_max_rss_kib', 'maximum', .20),
                ('database_sampled_max_bytes', 'maximum', .20),
                ('combined_observed_peaks_bytes', 'maximum', .20),
                ('runner_temporary_peak_bytes', 'maximum', .20),
                ('temporary_sampled_max_bytes', 'maximum', .20)):
            measured, original = item['measurements'][name], baseline['measurements'][name]
            if measured is None or original is None:
                checks[name] = {'status': 'not measured'}
                continue
            value, reference = measured[statistic], original[statistic]
            checks[name] = {**difference(value, reference), 'statistic': statistic,
                            'allowance_percent': allowance * 100,
                            'status': 'within observed target' if value <= reference * (1 + allowance) else 'review required'}
        for name in ('search', 'writer_wait'):
            measured, original = item[name], baseline[name]
            if measured is None or original is None:
                checks[name] = {'status': 'not measured'}
                continue
            enough = measured['at_least_100_observations'] and original['at_least_100_observations']
            value, reference = measured['p95_ns'], original['p95_ns']
            checks[name] = {**difference(value, reference), 'statistic': 'pooled p95',
                            'count': measured['count'], 'reference_count': original['count'],
                            'allowance_percent': 10,
                            'status': ('insufficient observations' if not enough else
                                       'within observed target' if value <= reference * 1.1 else 'review required')}
        results.append({'study': item['study'], 'version': item['version'],
                        'workload': item['workload'], 'reference': 'starting', 'checks': checks,
                        'search_errors': item['search_errors'], 'writer_errors': item['writer_errors']})
    return {'accepted_by_user': False,
            'note': 'Proposed thresholds, not an acceptance decision. Percentages near zero have no agreed absolute allowance. Sampled memory/space may miss peaks. Overlapping timing ranges require caution.',
            'results': results}


def counter_difference(samples, group, field):
    """Return an absent measurement as null, never as a measured zero."""
    first, last = samples[0].get(group), samples[-1].get(group)
    if not first or not last:
        return None
    if isinstance(first, dict):
        return sum(value.get(field, 0) for value in last.values()) - sum(
            value.get(field, 0) for value in first.values())
    return sum(value['value'] for value in last if value['op'].lower() == field) - sum(
        value['value'] for value in first if value['op'].lower() == field)

def write_tables(destination, settings, summaries):
    lines = ['# Synthetic comparison evidence', '',
             'Generated by `scripts/comparison/summarize.py`. See [the report](../../CYPHER_COMPARISON.md) for scope, explanations and stopping decisions.', '',
             'Times are seconds: middle result [fastest–slowest]. These are complete extension runs, including preparation and saving. First means first after restoring inputs, not an empty database cache. Phase medians are calculated separately and need not add to the complete-run median.', '',
             'Memory and temporary-space columns show the largest observation across repeats. Database memory includes cache and is sampled; combined memory adds separate observed peaks, which can miss the true peak between samples. Sampled temporary space can miss short-lived files. The runner also counts its own peak. Missing measurements are shown as —.', '',
             '[Individual CSV rows](measurements.csv), [full JSON measurements](measurements.json), [differences and percentages](comparisons.json), [proposed review targets](review-targets.json), [query and probe details](query-and-probe-details.json), [intermediate answer checks](intermediate-checks.json), [cross-database answer checks](cross-backend-checks.json), [source versions](source.json), and [stopped attempts](stopped-studies.json).', '']
    versions = ('starting', 'advanced', 'ordinary-reach', 'no-batch', 'ordinary-groups', 'ordinary-all')
    if (destination / 'batch-capacity.json').exists():
        lines.extend(['The separate [batching input-limit check](batch-capacity.json) and [CSV](batch-capacity.csv) retain successful runs and expected limit failures. These attempts are not part of the ESC1 timing tables. The [interrupted setup](batch-capacity-setup-stopped.json) and [final scope and restoration](completion.json) are recorded separately.', ''])
    workloads = ('first', 'full', 'unchanged', 'changed', 'one-support', 'last-support')
    lookup = {(item['study'], item['version'], item['workload']): item for item in summaries}
    def number(item, name, divisor=1, statistic='median'):
        value = item['measurements'].get(name)
        return '—' if value is None else f'{value[statistic] / divisor:.3f}'
    for study, configuration in settings.items():
        dataset, arguments = configuration['dataset'], configuration['arguments']
        lines.extend([f'## {study}', '',
                      f"{dataset['objects']:,} objects; {dataset['relationships']:,} relationships; {dataset['membership_pairs']:,} membership pairs; {dataset['template_authority_pairs']} template/authority combinations. Relevant input share: {dataset['relevant_share']:.4%}. Answer shape: {dataset['answer_shape']}. Search/writer probes: {arguments.get('probes', False)}. Batch size: {arguments.get('batch_rows', 500)}. Explicit PostgreSQL statistics preparation: {arguments.get('prepare_statistics', False)}.", '',
                      '| Version | First | Full | Unchanged | Changed | One support removed | Final support removed |',
                      '| --- | ---: | ---: | ---: | ---: | ---: | ---: |'])
        for version in versions:
            values = []
            for workload in workloads:
                item = lookup.get((study, version, workload))
                if item is None:
                    values.append('—')
                else:
                    value = item['measurements']['engine_ns']
                    values.append(f"{value['median']/1e9:.3f} [{value['minimum']/1e9:.3f}–{value['maximum']/1e9:.3f}]")
            lines.append('| ' + ' | '.join([version, *values]) + ' |')
        lines.extend(['', 'Full recalculation phases and processor time:', '',
                      '| Version | Prepare s | Calculate s | Save s | App processor s | Database processor s |',
                      '| --- | ---: | ---: | ---: | ---: | ---: |'])
        for version in versions:
            item = lookup[(study, version, 'full')]
            values = [number(item, name, divisor) for name, divisor in
                      (('preparation_ns', 1e9), ('calculation_ns', 1e9), ('saving_ns', 1e9),
                       ('app_cpu_seconds', 1), ('database_cpu_ns', 1e9))]
            lines.append('| ' + ' | '.join([version, *values]) + ' |')
        lines.extend(['', 'Full recalculation resources, in MiB:', '',
                      '| Version | App peak | Database sampled peak | Sum of observed peaks | Runner counted temporary peak | Sampled temporary peak |',
                      '| --- | ---: | ---: | ---: | ---: | ---: |'])
        for version in versions:
            item = lookup[(study, version, 'full')]
            values = [number(item, name, divisor, 'maximum') for name, divisor in
                      (('app_max_rss_kib', 1024), ('database_sampled_max_bytes', 2**20),
                       ('combined_observed_peaks_bytes', 2**20),
                       ('runner_temporary_peak_bytes', 2**20), ('temporary_sampled_max_bytes', 2**20))]
            lines.append('| ' + ' | '.join([version, *values]) + ' |')
        native = lookup[(study, 'native', 'first')]
        lines.extend(['', f"Native reference median: shared preparation {number(native, 'preparation_ns', 1e9)} s; ESC1 calculation {number(native, 'calculation_ns', 1e9)} s; answer checking {number(native, 'check_ns', 1e9)} s. No native connections were saved.", ''])
        if arguments.get('probes'):
            lines.extend(['Full recalculation probes, pooled across repeats:', '',
                          '| Version | Search count | Search 95th percentile ms | Writer count | Writer 95th percentile ms | Errors |',
                          '| --- | ---: | ---: | ---: | ---: | ---: |'])
            for version in versions:
                item = lookup[(study, version, 'full')]
                search, writer = item['search'], item['writer_wait']
                lines.append(f"| {version} | {search['count']} | {search['p95_ns']/1e6:.3f} | {writer['count']} | {writer['p95_ns']/1e6:.3f} | {item['search_errors'] + item['writer_errors']} |")
            lines.append('')
    (destination / 'README.md').write_text('\n'.join(lines) + '\n')


def summarize(studies, destination, attempts):
    records, details, settings, intermediate = [], [], {}, {}
    grouped = defaultdict(list)
    searches, writers = defaultdict(list), defaultdict(list)
    search_errors, writer_errors = defaultdict(int), defaultdict(int)
    baselines = {}
    for study in studies:
        directory = WORK / study
        complete = read(directory / 'complete.json')
        if not complete.get('complete'):
            raise ValueError(f'{study} has not completed')
        configuration = read(directory / 'settings.json')
        settings[study] = {
            'arguments': configuration['arguments'],
            'created_utc': configuration['created_utc'],
            'binary_sha256': configuration['binary_sha256'],
            'database_image': configuration['container']['Image'],
            'host_memory_bytes': configuration['memory']['MemTotal'],
            'dataset': read(directory / 'dataset.json'),
            'complete': complete,
            'query_sha256': {str(path.relative_to(directory / 'queries')): source_hash(path)
                             for path in sorted((directory / 'queries').glob('*/*')) if path.is_file()},
        }
        if (directory / 'advancement.json').exists():
            settings[study]['advancement'] = read(directory / 'advancement.json')
        if (directory / 'updated-dawgs.patch').exists():
            settings[study]['updated_dawgs_patch_sha256'] = source_hash(directory / 'updated-dawgs.patch')
        if (directory / 'orchestration.py').exists():
            settings[study]['orchestration_sha256'] = source_hash(directory / 'orchestration.py')
        if (directory / 'resume-note.json').exists():
            settings[study]['measurement_continuation'] = read(directory / 'resume-note.json')
        if (directory / 'measurement-source').exists():
            settings[study]['measurement_source_sha256'] = {
                path.name: source_hash(path) for path in sorted((directory / 'measurement-source').glob('*.go'))}
        if (directory / 'statistics-preparation.json').exists():
            settings[study]['statistics_preparation'] = read(directory / 'statistics-preparation.json')
        if (directory / 'storage-after-load.json').exists():
            storage = read(directory / 'storage-after-load.json')
            if 'data_and_logs_kib' in storage:
                # Keep the allocation, without exporting this machine's paths.
                settings[study]['allocated_data_and_log_bytes'] = sum(
                    int(line.split()[0]) * 1024 for line in storage['data_and_logs_kib'].splitlines())
            else:
                settings[study]['database_bytes_after_load'] = storage.get('database_bytes', storage.get('pg_database_bytes'))
        intermediate[study] = {}
        for path in sorted(directory.glob('*-intermediate.json')):
            intermediate[study][path.name] = read(path)
        for original in read(directory / 'measurements.json'):
            if not original['measured'] or original['action'] not in ('run', 'native'):
                if original['action'] == 'probe':
                    result = read(directory / (original['id'] + '.json'))
                    baselines[study] = result.get('observation')
                continue
            record = dict(original)
            result = read(directory / (record['id'] + '.json'))
            if settings[study].setdefault('limits', result['limits']) != result['limits']:
                raise ValueError(f'{study}: compared versions used different limits')
            samples = read(directory / (record['id'] + '-resources.json'))
            record['study'] = study
            record['combined_observed_peaks_bytes'] = (
                record['app_max_rss_kib'] * 1024 + record['database_sampled_max_bytes'])
            # This sums separate peaks, not simultaneous resident memory. The
            # database number includes cache; do not describe it as an exact peak.
            record['database_read_bytes'] = counter_difference(samples, 'database_io', 'read')
            record['database_write_bytes'] = counter_difference(samples, 'database_io', 'write')
            record['database_received_bytes'] = counter_difference(samples, 'database_network', 'rx_bytes')
            record['database_sent_bytes'] = counter_difference(samples, 'database_network', 'tx_bytes')
            counters = directory / (record['id'] + '-database-counters.json')
            if counters.exists():
                record['postgres_counters'] = read(counters)['difference']
                record['postgres_temporary_bytes_written'] = record['postgres_counters']['temp_bytes']
                record['postgres_temporary_files'] = record['postgres_counters']['temp_files']
            report = result.get('report') or {}
            record['query_reports'] = (report.get('execution') or {}).get('queries')
            observation = result.get('observation') or {}
            key = (study, record['version'], record['workload'])
            grouped[key].append(record)
            searches[key].extend(observation.get('search_nanoseconds') or [])
            writers[key].extend(observation.get('writer_wait_nanoseconds') or [])
            search_errors[key] += observation.get('search_errors', 0)
            writer_errors[key] += observation.get('writer_errors', 0)
            # Exact answer rows remain in the private evidence. Export the
            # execution report and counts without repeating thousands of pairs.
            compact_report = {name: value for name, value in report.items()
                              if name != 'desired_relationships'}
            details.append({'study': study, 'id': record['id'], 'report': compact_report,
                            'observation': observation})
            records.append(record)
    measures = ('engine_ns', 'operation_ns', 'check_ns', 'preparation_ns',
                'calculation_ns', 'saving_ns', 'app_max_rss_kib', 'app_cpu_seconds',
                'database_sampled_max_bytes', 'combined_observed_peaks_bytes',
                'database_cpu_ns', 'temporary_sampled_max_bytes',
                'runner_temporary_peak_bytes', 'database_read_bytes',
                'database_write_bytes', 'database_received_bytes', 'database_sent_bytes',
                'postgres_temporary_bytes_written', 'postgres_temporary_files')
    summaries = []
    for key, observations in sorted(grouped.items()):
        study, version, workload = key
        summaries.append({
            'study': study, 'version': version, 'workload': workload,
            'repeats': len(observations),
            'measurements': {name: middle_and_range([item.get(name) for item in observations])
                             for name in measures},
            'search': latency(searches[key]), 'writer_wait': latency(writers[key]),
            'search_errors': search_errors[key], 'writer_errors': writer_errors[key],
            'answer_hashes': sorted({item['answer_sha256'] for item in observations}),
        })
    destination.mkdir(parents=True, exist_ok=True)
    manifest = read(WORK / 'starting-manifest.json')
    source = {name: manifest[name] for name in
              ('commits', 'source_archives', 'go_version', 'cpu_count', 'experiment_budget_hours')}
    source['reference_instrumentation_sha256'] = source_hash(
        ROOT / 'scripts/comparison/reference-instrumentation.patch')
    lookup = {(item['study'], item['version'], item['workload']): item for item in summaries}
    comparisons = []
    for item in summaries:
        if item['version'] not in ('ordinary-reach', 'no-batch', 'ordinary-groups', 'ordinary-all', 'advanced'):
            continue
        references = ['advanced'] if item['version'] != 'advanced' else []
        if item['version'] in ('advanced', 'ordinary-all'):
            references.append('starting')
        for reference in references:
            baseline = lookup.get((item['study'], reference, item['workload']))
            if baseline is None:
                continue
            differences = {}
            for name in measures:
                value, original = item['measurements'][name], baseline['measurements'][name]
                if value is not None and original is not None:
                    difference = value['median'] - original['median']
                    differences[name] = {'reference_median': original['median'],
                                         'comparison_median': value['median'],
                                         'difference': difference,
                                         'percent': difference / original['median'] * 100 if original['median'] else None}
            comparisons.append({'study': item['study'], 'version': item['version'],
                                'workload': item['workload'], 'reference': reference,
                                'measurements': differences})
    outputs = {'source.json': source, 'settings.json': settings, 'measurements.json': records,
               'summaries.json': summaries, 'query-and-probe-details.json': details,
               'intermediate-checks.json': intermediate, 'search-baselines.json': baselines,
               'comparisons.json': comparisons, 'review-targets.json': review_targets(summaries)}
    # Internal object numbers can differ between databases. Saved-output
    # fingerprints use stable object identifiers and complete saved properties.
    # Compare those, rather than assuming that equal counts mean equal answers.
    across_databases = {}
    for study, configuration in settings.items():
        label, backend = study.rsplit('/', 1)
        across_databases.setdefault(label, {})[backend] = configuration['complete']['expected_hashes']
    cross_checks = {}
    for label, answers in across_databases.items():
        if 'pg' not in answers or 'neo4j' not in answers:
            cross_checks[label] = {'checked': False, 'reason': 'both database studies were not selected'}
            continue
        if answers['pg'] != answers['neo4j']:
            raise ValueError(f'{label}: stable saved answers differ between databases')
        cross_checks[label] = {'checked': True, 'matched': True, 'expected_hashes': answers['pg']}
    outputs['cross-backend-checks.json'] = cross_checks
    # Deliberately stopped studies stay separate: a failed/cancelled attempt
    # has no completion time or speed ratio. Keep the reason beside its data.
    stopped = {}
    for study in attempts:
        directory = WORK / study
        stopped[study] = {
            'decision': read(directory / 'stopping-decision.json'),
            'dataset': read(directory / 'dataset.json'),
            'arguments': read(directory / 'settings.json')['arguments'],
            'attempts': [item for item in read(directory / 'measurements.json')
                         if item['action'] in ('run', 'native')],
        }
    outputs['stopped-studies.json'] = stopped
    for name, value in outputs.items():
        (destination / name).write_text(json.dumps(value, indent=2) + '\n')
    write_tables(destination, settings, summaries)
    fields = sorted({key for record in records for key in record
                     if not any(isinstance(item.get(key), (list, dict)) for item in records)})
    with (destination / 'measurements.csv').open('w') as output:
        writer = csv.DictWriter(output, fieldnames=fields, extrasaction='ignore')
        writer.writeheader()
        writer.writerows(records)
    print(f'Published {len(records)} measured runs in {destination}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('studies', nargs='+', help='label/backend paths under private work/cypher-comparison')
    parser.add_argument('--output', type=Path, default=ROOT / 'evidence/cypher-comparison')
    parser.add_argument('--attempts', nargs='*', default=[], help='separate stopped studies with a written stopping decision')
    arguments = parser.parse_args()
    for study in arguments.studies + arguments.attempts:
        if '..' in Path(study).parts or Path(study).is_absolute():
            parser.error('use relative label/backend paths')
    summarize(arguments.studies, arguments.output, arguments.attempts)
