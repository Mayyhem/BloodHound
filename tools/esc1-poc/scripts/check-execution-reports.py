#!/usr/bin/env python3
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
"""Check report states in an isolated browser, without a graph connection."""
import argparse
import copy
import json
import os
from pathlib import Path
from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--library-path')
    parser.add_argument('--output', default='work/execution-report-browser')
    args = parser.parse_args()
    environment = dict(os.environ)
    if args.library_path:
        environment['LD_LIBRARY_PATH'] = args.library_path
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    errors = []
    revision = 'a' * 64
    status = {'state': 'failed', 'revision': revision, 'saved_revision': 'b' * 64,
              'results_stale': True, 'backend': 'pg', 'message': 'Previous results were kept.',
              'execution': {'calculation': 'failed', 'save': 'not-started', 'phase': 'read-query',
                  'failure': {'query': 'result.cypher', 'stage': 2, 'phase': 'read-query',
                              'message': '<img src=x onerror=window.injected=true> The query divided by zero.'},
                  'proposed': {'relationships_added': 2},
                  'queries': [{'name': 'result.cypher', 'stage': 2, 'state': 'failed', 'rows': 4,
                               'elapsed_nanoseconds': 12000000, 'proposed_outputs': 2,
                               'reason': 'A confirmed input change affects this calculation.'}]}}
    status['history'] = [{key: copy.deepcopy(value) for key, value in status.items()}]
    status['history'][0]['updated_at'] = '2026-10-12T12:00:00Z'
    unavailable = False
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(env=environment)
        page = browser.new_page(viewport={'width': 1500, 'height': 1000})
        page.on('pageerror', lambda error: errors.append(str(error)))

        def route(request):
            path = request.request.url.split('/')[-1]
            if not path:
                request.fulfill(content_type='text/html', body=(ROOT / 'internal/ruleservice/upload.html').read_text())
                return
            if path == 'status' and unavailable:
                request.fulfill(status=503, content_type='application/json', body=json.dumps({'error': 'Report service unavailable'}))
                return
            value = {
                'browser-mode': {'local_ui': True},
                'rules': {'revision': revision, 'files': {}, 'disabled': []},
                'plan': {'revision': revision, 'queries': [], 'extensions': [], 'threads': []},
                'draft': {'revision': '', 'files': {}, 'ready': False},
                'analysis': {'available': False, 'message': 'No graph connection in this test.'},
                'status': status,
            }.get(path)
            assert value is not None, path
            request.fulfill(content_type='application/json', body=json.dumps(value))

        page.route('**/*', route)
        page.goto('http://reports.test/')
        page.get_by_text('Latest analysis report', exact=True).click()
        report = page.locator('#execution-report')
        expect(report).to_contain_text('result.cypher (stage 2)')
        expect(report).to_contain_text('12.0 ms')
        expect(report).to_contain_text('unconfirmed')
        expect(report).to_contain_text('Last confirmed saved revision: ' + 'b' * 64)
        assert report.locator('img').count() == 0
        assert not page.evaluate('Boolean(window.injected)')
        report.locator('details').first.locator('summary').click()
        expect(report.locator('details').first).to_contain_text('The query divided by zero.')
        page.screenshot(path=str(output / 'failed.png'), full_page=True)
        failed_color = page.locator('#analysis-state .status-light').evaluate('(item) => getComputedStyle(item).backgroundColor')
        status.update(state='uncertain', results_uncertain=True)
        status['execution'].update(calculation='succeeded', save='uncertain')
        page.locator('#refresh').click()
        expect(report).to_contain_text('The save outcome is unknown.')
        uncertain_color = page.locator('#analysis-state .status-light').evaluate('(item) => getComputedStyle(item).backgroundColor')
        assert uncertain_color != failed_color
        expect(report).to_contain_text('Calculation: succeeded. Saving: uncertain.')
        status.update(state='success', saved_revision=revision, results_uncertain=False, results_stale=False)
        status['execution'].update(save='succeeded', saved={'relationships_added': 2})
        status['execution'].pop('failure')
        status['execution']['queries'][0].update(state='succeeded', saved_outputs=2)
        page.locator('#refresh').click()
        expect(report).to_contain_text('These changes were saved.')
        success_color = page.locator('#analysis-state .status-light').evaluate('(item) => getComputedStyle(item).backgroundColor')
        assert success_color not in (failed_color, uncertain_color)
        unavailable = True
        page.locator('#refresh').click()
        expect(page.locator('#analysis-state')).to_contain_text('unavailable')
        expect(report).to_contain_text('The execution report could not be loaded.')
        assert not errors, errors
        browser.close()
    (output / 'checks.json').write_text(json.dumps({'passed': True, 'checks': ['query details', 'history', 'old saved revision', 'uncertain save', 'confirmed save', 'unavailable report', 'escaped error text']}, indent=2) + '\n')
    print('Execution report browser checks passed.')


if __name__ == '__main__':
    main()
