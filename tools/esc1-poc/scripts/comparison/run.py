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
"""Repeat the comparison on the dedicated local databases; never deploy an app.

Each version starts from the same generated input and no saved extension result.
The following workloads then use that version's own saved result. Exact source
and output copies are retained in work/cypher-comparison, outside Git.
"""
import argparse
import csv
import datetime
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
WORK = ROOT / 'work/cypher-comparison'
VARIANTS = ('advanced', 'ordinary-reach', 'no-batch', 'ordinary-groups', 'ordinary-all')

def file_sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1048576), b''):
            digest.update(block)
    return digest.hexdigest()

def temporary_bytes(directory):
    def inspect_error(error):
        # A completed calculation removes its whole temporary directory.
        # Ignore that normal disappearance, but retain other read failures.
        if not isinstance(error, FileNotFoundError):
            raise error

    total = 0
    for parent, _, filenames in os.walk(directory, onerror=inspect_error):
        for filename in filenames:
            try:
                total += (Path(parent) / filename).stat().st_size
            except FileNotFoundError:
                # Files can disappear between listing and reading their size.
                continue
    return total

class DockerConnection(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect('/var/run/docker.sock')

def docker(path):
    connection = DockerConnection('localhost', timeout=10)
    connection.request('GET', '/v1.41/' + path)
    response = connection.getresponse()
    value = json.loads(response.read())
    connection.close()
    if response.status != 200:
        raise RuntimeError(f'Docker read failed: {response.status}')
    return value

def memory():
    fields = {}
    for line in Path('/proc/meminfo').read_text().splitlines():
        key, value = line.split(':', 1)
        fields[key] = int(value.strip().split()[0]) * 1024
    return fields

def swap_out_pages():
    # Available memory can recover because Linux is moving memory to disk.
    # Stop if that continues. Reading old pages back with ample free memory is
    # recovery, not new pressure; counting those reads caused false stops.
    counters = dict(line.split() for line in Path('/proc/vmstat').read_text().splitlines())
    return int(counters['pswpout'])

def safety():
    available = memory()
    disk = shutil.disk_usage(WORK)
    if disk.free < disk.total * .25:
        raise RuntimeError('storage reached the 25% free reserve')
    if available['MemAvailable'] < available['MemTotal'] * .20:
        raise RuntimeError('memory reached the 20% available reserve')
    # The recorded total budget covers tests, loading, runs, checks and cleanup.
    manifest = json.loads((WORK / 'starting-manifest.json').read_text())
    start = datetime.datetime.fromisoformat(manifest['created_utc'].replace('Z', '+00:00'))
    deadline = start + datetime.timedelta(hours=manifest['experiment_budget_hours'])
    if datetime.datetime.now(datetime.timezone.utc) > deadline:
        raise RuntimeError('the recorded 24-hour experiment budget has ended')

def variants():
    for variant in VARIANTS:
        destination = WORK / 'variants' / variant
        destination.mkdir(parents=True, exist_ok=True)
        for source in (ROOT / 'extension').glob('*'):
            if source.suffix in ('.cypher', '.json'):
                shutil.copyfile(source, destination / source.name)
        if variant in ('ordinary-reach', 'ordinary-all'):
            for name in ('membership_reach', 'forest_reach', 'ca_root_reach', 'ca_auth_reach'):
                shutil.copyfile(ROOT / 'examples/esc1-cypher' / (name + '.cypher'), destination / (name + '.cypher'))
        if variant in ('ordinary-groups', 'ordinary-all'):
            content = (ROOT / 'examples/esc1-cypher/esc1.cypher').read_text()
            if variant == 'ordinary-groups':
                content = '// batch-on: trusted_cas.cypher\n' + content
            (destination / 'esc1.cypher').write_text(content)
        if variant == 'no-batch':
            path = destination / 'esc1.cypher'
            path.write_text('\n'.join(line for line in path.read_text().splitlines() if not line.startswith('// batch-on:')) + '\n')

class Experiment:
    def __init__(self, arguments):
        self.arguments = arguments
        self.directory = WORK / arguments.label / arguments.backend
        self.directory.mkdir(parents=True, exist_ok=False)
        shutil.copytree(WORK / 'variants', self.directory / 'queries')
        shutil.copyfile(Path(__file__), self.directory / 'orchestration.py')
        shutil.copytree(ROOT / 'cmd/cypher-comparison', self.directory / 'measurement-source')
        # Preserve the source difference as well as the binary fingerprints.
        # DAWGS is a separate checkout, so the parent repository cannot record it.
        (self.directory / 'updated-dawgs.patch').write_bytes(subprocess.check_output(
            ['git', 'diff', '--binary'], cwd=ROOT.parent / 'DAWGS'))
        self.container = 'esc1-poc-test-pg-graph-1' if arguments.backend == 'pg' else 'esc1-poc-test-graph-1'
        self.environment = dict(os.environ)
        self.environment['CONNECTION_STRING'] = ('postgresql://bloodhound:experiment-only@127.0.0.1:25432/cypher_comparison' if arguments.backend == 'pg' else 'neo4j://neo4j:experiment-only@127.0.0.1:27687')
        self.environment['TMPDIR'] = str(self.directory / 'temporary')
        Path(self.environment['TMPDIR']).mkdir()
        self.records = []
        self.expected = {}
        self.intermediate_reference = None
        self.sequence = 0
        self.saved_identity = hashlib.sha256(b'').hexdigest()
        self.save('settings.json', {'binary_sha256': {name: file_sha256(WORK / name) for name in ('measure', 'measure-reference')}, 'arguments': vars(arguments), 'created_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'memory': memory(), 'storage': shutil.disk_usage(WORK)._asdict(), 'container': {k: v for k, v in docker('containers/' + self.container + '/json').items() if k in ('Image', 'Name', 'Id')}})

    def save(self, filename, value):
        (self.directory / filename).write_text(json.dumps(value, indent=2) + '\n')

    def database_counters(self):
        if self.arguments.backend != 'pg': return None
        columns = ('temp_bytes', 'temp_files', 'blks_read', 'blks_hit', 'tup_returned', 'tup_fetched', 'tup_inserted', 'tup_updated', 'tup_deleted')
        expression = ','.join("'%s',%s" % (name, name) for name in columns)
        statement = "SELECT json_build_object(%s) FROM pg_stat_database WHERE datname='cypher_comparison'" % expression
        value = subprocess.check_output(['docker', 'exec', self.container, 'psql', '-U', 'bloodhound', '-d', 'postgres', '-Atc', statement], text=True)
        return json.loads(value)

    def sample(self):
        value = docker('containers/' + self.container + '/stats?stream=false&one-shot=true')
        usage = value.get('memory_stats', {})
        available = memory()
        return {'time': time.monotonic(), 'database_memory_bytes': usage.get('usage', 0), 'database_inactive_file_bytes': usage.get('stats', {}).get('inactive_file', 0), 'database_cpu_nanoseconds': value.get('cpu_stats', {}).get('cpu_usage', {}).get('total_usage', 0), 'database_io': value.get('blkio_stats', {}).get('io_service_bytes_recursive'), 'database_network': value.get('networks'), 'host_available_bytes': available['MemAvailable'], 'host_swap_used_bytes': available['SwapTotal'] - available['SwapFree'], 'temporary_bytes': temporary_bytes(Path(self.environment['TMPDIR']))}

    def run(self, name, action='run', version='advanced', repeat=0, changes=None, measured=True):
        safety()
        self.sequence += 1
        identifier = f'{self.sequence:04d}-{version}-{repeat}-{name}'
        output = self.directory / (identifier + '.json')
        binary = WORK / ('measure-reference' if version == 'starting' else 'measure')
        directory = self.directory / 'queries' / ('advanced' if version == 'starting' else version)
        if version == 'starting':
            directory = WORK / 'starting-source/tools/esc1-poc/extension'
        command = [str(binary), '-action', action, '-extension', str(directory), '-objects', str(self.arguments.objects), '-members', str(self.arguments.members), '-authorities', str(self.arguments.authorities), '-answer-shape', self.arguments.shape, '-batch-rows', str(self.arguments.batch_rows)]
        if self.arguments.probes and action == 'run':
            command += ['-probes']
        if changes:
            path = self.directory / (identifier + '-changes.json')
            path.write_text(json.dumps(changes))
            command += ['-changes', str(path)]
        environment = dict(self.environment)
        environment['COMPARISON_ANSWERS'] = str(self.directory / (identifier + '.answers'))
        if name == 'answer-check':
            environment['COMPARISON_INTERMEDIATE'] = str(self.directory / (identifier + '-intermediate'))
        database_before = self.database_counters()
        samples = [self.sample()]
        previous_swap_pages = swap_out_pages()
        consecutive_swap_samples = 0
        started = time.monotonic()
        with output.open('w') as stdout, (self.directory / (identifier + '.log')).open('w') as stderr:
            process = subprocess.Popen(['/usr/bin/time', '-f', '%M %U %S %e %I %O', '-o', str(self.directory / (identifier + '.time')), *command], cwd=ROOT, env=environment, stdout=stdout, stderr=stderr, start_new_session=True)
            try:
                while process.poll() is None:
                    time.sleep(.5)
                    safety()
                    samples.append(self.sample())
                    current_swap_pages = swap_out_pages()
                    if current_swap_pages > previous_swap_pages:
                        consecutive_swap_samples += 1
                    else:
                        consecutive_swap_samples = 0
                    previous_swap_pages = current_swap_pages
                    if consecutive_swap_samples >= 3:
                        raise RuntimeError('host continued moving memory to swap across three samples')
                    if time.monotonic() - started > 2 * 3600 + 10 * 60:
                        raise RuntimeError('run exceeded the fixed two-hour allowance plus cleanup')
            except BaseException as failure:
                os.killpg(process.pid, signal.SIGTERM)
                try: process.wait(timeout=10)
                except subprocess.TimeoutExpired: os.killpg(process.pid, signal.SIGKILL); process.wait()
                self.save(identifier + '-resources.json', samples)
                self.save(identifier + '-stopped.json', {'reason': str(failure), 'elapsed_seconds': time.monotonic() - started, 'completed': False})
                raise
        value = json.loads(output.read_text())
        samples.append(self.sample())
        self.save(identifier + '-resources.json', samples)
        database_after = self.database_counters()
        if database_before is not None:
            self.save(identifier + '-database-counters.json', {'before': database_before, 'after': database_after, 'difference': {key: database_after[key] - database_before[key] for key in database_before}})
        usage = (self.directory / (identifier + '.time')).read_text().splitlines()[-1].split()
        report = value.get('report') or {}
        record = {'id': identifier, 'backend': self.arguments.backend, 'label': self.arguments.label, 'version': version, 'repeat': repeat, 'workload': name, 'action': action, 'measured': measured, 'exit_code': process.returncode, 'engine_ns': report.get('elapsed_nanoseconds', value.get('operation_nanoseconds')), 'operation_ns': value['operation_nanoseconds'], 'check_ns': value['check_nanoseconds'], 'preparation_ns': report.get('preparation_nanoseconds', value['preparation_nanoseconds']), 'calculation_ns': report.get('calculation_nanoseconds', value['calculation_nanoseconds']), 'saving_ns': report.get('saving_nanoseconds'), 'app_max_rss_kib': int(usage[0]), 'app_cpu_seconds': float(usage[1]) + float(usage[2]), 'database_sampled_max_bytes': max(s['database_memory_bytes'] for s in samples), 'database_cpu_ns': samples[-1]['database_cpu_nanoseconds'] - samples[0]['database_cpu_nanoseconds'], 'temporary_sampled_max_bytes': max(s['temporary_bytes'] for s in samples), 'runner_temporary_peak_bytes': report.get('temporary_peak_bytes'), 'connections': value['connections'], 'answer_sha256': value['answer_sha256'], 'saved_outputs_sha256': value.get('saved_outputs_sha256'), 'saved_identity_sha256': value.get('saved_identity_sha256'), 'run_record_sha256': value.get('run_record_sha256'), 'skipped': report.get('skipped'), 'error': value.get('error'), 'queries': report.get('queries')}
        if action == 'run' and process.returncode:
            record['previous_saved_identity_sha256'] = self.saved_identity
            record['previous_saved_state_preserved'] = value.get('saved_identity_sha256') == self.saved_identity
        if not process.returncode:
            if action in ('reset', 'clear', 'load'):
                self.saved_identity = hashlib.sha256(b'').hexdigest()
            elif action == 'run':
                self.saved_identity = value['saved_identity_sha256']
        self.records.append(record)
        self.save('measurements.json', self.records)
        print(json.dumps({key: record[key] for key in ('id', 'engine_ns', 'connections', 'error')}), flush=True)
        if value.get('observation', {}).get('search_errors') or value.get('observation', {}).get('writer_errors'):
            raise RuntimeError(f'{identifier}: a search or writer probe failed')
        if process.returncode:
            raise RuntimeError(f'{identifier} failed; advancement stopped; inspect its saved state and error')
        if measured and action in ('run', 'native'):
            expected_count = 0 if name in ('changed', 'last-support') else (self.arguments.members if self.arguments.shape == 'users' else 21)
            if expected_count is not None and value['connections'] != expected_count:
                raise RuntimeError(f'{identifier}: expected {expected_count} connections, got {value["connections"]}')
            key = 'empty' if name in ('changed', 'last-support') else 'full'
            hashes = (value['answer_sha256'], value.get('saved_outputs_sha256'))
            previous = self.expected.setdefault(key, hashes)
            if previous[0] != hashes[0] or (action == 'run' and previous[1] and previous[1] != hashes[1]):
                raise RuntimeError(f'{identifier}: exact answer mismatch; compare retained .answers files')
        if name == 'answer-check':
            hashes = {}
            folder = Path(environment['COMPARISON_INTERMEDIATE'])
            for query, count in report['query_rows'].items():
                path = folder / (query + '.jsonl')
                if not path.exists(): path.touch()
                subprocess.run(['sort', '-S', '8M', '-T', self.environment['TMPDIR'], '-o', str(path), str(path)], env={**os.environ, 'LC_ALL': 'C'}, check=True)
                digest = hashlib.sha256()
                with path.open('rb') as file:
                    for block in iter(lambda: file.read(1048576), b''): digest.update(block)
                hashes[query] = {'rows': count, 'sha256': digest.hexdigest()}
            self.save(identifier + '-intermediate.json', hashes)
            if self.intermediate_reference is None: self.intermediate_reference = hashes
            elif hashes != self.intermediate_reference:
                raise RuntimeError(f'{identifier}: intermediate answers differ; retained sorted rows show exact differences')
        return value

    def execute(self):
        if self.arguments.objects >= 1_000_000:
            if not all((self.arguments.projected_memory_bytes, self.arguments.projected_storage_bytes, self.arguments.projected_seconds, self.arguments.projection_note)):
                raise RuntimeError('large loads require recorded projections from the preceding smaller checks')
            total_memory = memory()['MemTotal']
            storage = shutil.disk_usage(WORK)
            manifest = json.loads((WORK / 'starting-manifest.json').read_text())
            deadline = datetime.datetime.fromisoformat(manifest['created_utc'].replace('Z', '+00:00')) + datetime.timedelta(hours=manifest['experiment_budget_hours'])
            available_seconds = (deadline - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
            decision = {'projected_combined_memory_bytes': self.arguments.projected_memory_bytes, 'projected_storage_including_copies_bytes': self.arguments.projected_storage_bytes, 'projected_load_runs_checks_cleanup_seconds': self.arguments.projected_seconds, 'note': self.arguments.projection_note, 'memory_pass': 2 * self.arguments.projected_memory_bytes <= .8 * total_memory, 'storage_pass': 2 * self.arguments.projected_storage_bytes <= storage.free - .25 * storage.total, 'time_pass': self.arguments.projected_seconds <= available_seconds}
            self.save('advancement.json', decision)
            if not all(decision[key] for key in ('memory_pass', 'storage_pass', 'time_pass')):
                raise RuntimeError('advancement projection exceeded a reserve or the remaining budget')
            manifest['large_data_started'] = True
            manifest.setdefault('large_steps', []).append({'study': self.arguments.label + '/' + self.arguments.backend,
                                                          'objects': self.arguments.objects, 'projection': decision})
            (WORK / 'starting-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        if self.arguments.replace:
            self.run('clear', action='clear', measured=False)
        loaded = self.run('load', action='load', measured=False)
        self.save('dataset.json', loaded['dataset'])
        if self.arguments.backend == 'pg':
            size = subprocess.check_output(['docker', 'exec', self.container, 'psql', '-U', 'bloodhound',
                                            '-d', 'postgres', '-Atc', "SELECT pg_database_size('cypher_comparison')"], text=True)
            self.save('storage-after-load.json', {'database_bytes': int(size)})
        else:
            size = subprocess.check_output(['du', '-sk', str(ROOT / 'work/test-neo4j'),
                                            str(ROOT / 'work/test-neo4j-logs')], text=True)
            self.save('storage-after-load.json', {'data_and_logs_kib': size, 'includes_previously_allocated_store_space': True})
        if self.arguments.prepare_statistics and self.arguments.backend == 'pg':
            # A fresh large load can change PostgreSQL's row-count estimates
            # during the first repeats. Prepare them once, outside run timing,
            # and record this explicit condition for every compared version.
            started = time.monotonic()
            subprocess.run(['docker', 'exec', self.container, 'psql', '-U', 'bloodhound',
                            '-d', 'cypher_comparison', '-v', 'ON_ERROR_STOP=1',
                            '-c', 'ANALYZE node; ANALYZE edge;'], check=True, capture_output=True)
            self.save('statistics-preparation.json', {'elapsed_seconds': time.monotonic() - started})
        if self.arguments.probes:
            self.run('search-baseline', action='probe', measured=False)
        for repeat in range(1, self.arguments.rounds + 1):
            order = ['starting', *VARIANTS, 'native']
            if repeat % 2 == 0: order.reverse()
            for version in order:
                self.run('restore-inputs', action='reset', measured=False)
                if version == 'native':
                    self.run('first', action='native', version='native', repeat=repeat)
                    continue
                self.run('first', version=version, repeat=repeat)
                self.run('full', version=version, repeat=repeat)
                if self.arguments.full_only: continue
                before = self.run('unchanged', version=version, repeat=repeat, changes={'coverage_known': True, 'nodes': [], 'relationships': []})
                self.run('apply-change', action='change', measured=False)
                self.run('changed', version=version, repeat=repeat, changes={'coverage_known': True, 'nodes': ['CertTemplate'], 'relationships': []})
                self.run('restore-inputs', action='reset', measured=False)
                self.run('prepare-removal', version=version, repeat=repeat, measured=False)
                self.run('remove-first-reason', action='remove-one', measured=False)
                self.run('one-support', version=version, repeat=repeat, changes={'coverage_known': True, 'nodes': [], 'relationships': ['PublishedTo']})
                self.run('remove-final-reason', action='remove-last', measured=False)
                self.run('last-support', version=version, repeat=repeat, changes={'coverage_known': True, 'nodes': [], 'relationships': ['PublishedTo']})
        for version in ['starting', *VARIANTS]:
            self.run('restore-inputs', action='reset', measured=False)
            self.run('answer-check', version=version, measured=False)
        self.run('restore-inputs', action='reset', measured=False)
        fields = sorted({key for record in self.records for key in record if key != 'queries'})
        with (self.directory / 'measurements.csv').open('w') as file:
            writer = csv.DictWriter(file, fieldnames=fields, extrasaction='ignore')
            writer.writeheader(); writer.writerows(self.records)
        self.save('complete.json', {'complete': True, 'measurements': len(self.records), 'expected_hashes': self.expected})

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend', choices=('pg', 'neo4j'), required=True)
    parser.add_argument('--label', required=True)
    parser.add_argument('--objects', type=int, required=True)
    parser.add_argument('--members', type=int, required=True)
    parser.add_argument('--authorities', type=int, default=2)
    parser.add_argument('--shape', choices=('users', 'groups'), default='users')
    parser.add_argument('--rounds', type=int, default=3)
    parser.add_argument('--batch-rows', type=int, default=500, help='same batch size for every version; separate changed-limit studies explicitly')
    parser.add_argument('--replace', action='store_true', help='clear only a graph verified as belonging to this generator')
    parser.add_argument('--probes', action='store_true')
    parser.add_argument('--full-only', action='store_true', help='separate repeated first/full comparisons; other workload checks remain in the complete matrix')
    parser.add_argument('--prepare-statistics', action='store_true', help='explicitly ANALYZE PostgreSQL after loading, before all compared versions')
    parser.add_argument('--projected-memory-bytes', type=int)
    parser.add_argument('--projected-storage-bytes', type=int)
    parser.add_argument('--projected-seconds', type=int)
    parser.add_argument('--projection-note')
    arguments = parser.parse_args()
    if not arguments.label.replace('-', '').replace('_', '').isalnum(): parser.error('label must be a simple folder name')
    if arguments.rounds < 3: parser.error('matched comparisons need at least three repeats')
    variants()
    Experiment(arguments).execute()
