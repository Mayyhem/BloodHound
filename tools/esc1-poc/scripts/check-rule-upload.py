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
"""Check rule previews without writes, or test activation on an empty isolated service."""

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shlex
import urllib.error
import urllib.parse
import urllib.request

from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *arguments, **keywords):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', default='http://127.0.0.1:18081')
    parser.add_argument('--settings', type=Path, default=ROOT / 'rules.env')
    parser.add_argument('--browser')
    parser.add_argument('--library-path')
    parser.add_argument('--output', type=Path, default=ROOT / 'work' / 'rule-upload-browser')
    parser.add_argument('--bearer-mode', action='store_true', help='Override browser mode discovery to exercise bearer-token fallback against the authenticated API.')
    parser.add_argument('--isolated', action='store_true', help='Allow activation tests only on a fresh, dedicated service with no active rules.')
    arguments = parser.parse_args()
    parsed = urllib.parse.urlparse(arguments.url)
    if parsed.scheme != 'http' or parsed.hostname not in ('localhost', '127.0.0.1', '::1') or parsed.path not in ('', '/') or parsed.username or parsed.query or parsed.fragment:
        raise SystemExit('Use the dedicated loopback registry address.')
    address = arguments.url.rstrip('/')
    directory = arguments.output.resolve()
    directory.relative_to(ROOT / 'work')
    directory.mkdir(parents=True, exist_ok=True)
    token = None
    for line in arguments.settings.read_text().splitlines():
        if line.startswith('POSTPROCESSOR_RULE_TOKEN='):
            values = shlex.split(line.split('=', 1)[1])
            if len(values) != 1 or token is not None:
                raise SystemExit('Settings must contain one bearer token.')
            token = values[0]
    if token is None or len(token) < 32:
        raise SystemExit('Settings need a bearer token of at least 32 characters.')
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirects())

    def api(path, method='GET', body=None):
        request = urllib.request.Request(address + '/v1/' + path, method=method,
            headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'},
            data=None if body is None else json.dumps(body).encode())
        with client.open(request, timeout=15) as response:
            return json.loads(response.read((4 << 20) + 1))

    def active():
        try:
            return api('rules')
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return None
            raise

    before = active()
    if arguments.isolated and before is not None:
        raise SystemExit('--isolated requires an empty service; refusing to modify existing rules.')
    candidate = dict(before['files']) if before else {path.name: path.read_text() for path in (ROOT / 'examples' / 'cross-platform' / 'extension').iterdir() if path.name.endswith('.json') or path.suffix == '.cypher'}
    filename = sorted(name for name in candidate if name.endswith('.cypher'))[0]
    results = {'checked_at_utc': datetime.now(timezone.utc).isoformat(), 'revision': before['revision'] if before else None, 'isolated': arguments.isolated, 'bearer_mode': arguments.bearer_mode, 'checks': [], 'browser_errors': []}
    environment = dict(os.environ)
    if arguments.library_path:
        environment['LD_LIBRARY_PATH'] = arguments.library_path

    def payloads(files):
        return [{'name': name, 'mimeType': 'text/plain', 'buffer': contents.encode()} for name, contents in files.items()]

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(executable_path=arguments.browser, env=environment)
        page = browser.new_page(viewport={'width': 1440, 'height': 1000})
        page.on('pageerror', lambda error: results['browser_errors'].append(str(error)))
        page.route('**/*', lambda route: route.continue_() if route.request.url.startswith(address + '/') or route.request.url.startswith('blob:') else route.abort())
        if arguments.bearer_mode:
            page.route(address + '/v1/browser-mode', lambda route: route.fulfill(status=200, content_type='application/json', body=json.dumps({'local_ui': False})))

        def connect():
            page.goto(address + '/', wait_until='networkidle')
            if not api('browser-mode')['local_ui'] or arguments.bearer_mode:
                page.locator('#token').fill(token)
                page.locator('#refresh').click()
            expect(page.locator('#message')).to_contain_text('up to date')

        def refreshed():
            page.locator('#refresh').click()
            expect(page.locator('#message')).to_contain_text('up to date')

        def upload(files, overwrite=()):
            if not page.locator('#files').is_visible():
                page.locator('#upload-controls > summary').click()
            page.locator('#files').set_input_files(payloads(files))
            page.locator('#preview').click()
            for name in overwrite:
                expect(page.locator('#overwrite-name')).to_have_text(name)
                page.locator('#overwrite-file').click()
            expect(page.locator('#review')).to_be_visible()

        def activate():
            page.locator('#activate').click()
            expect(page.locator('#message')).to_contain_text('activated')

        def row(name):
            return page.locator('#rule-rows > tr').filter(has=page.locator('[data-select-rule="' + name + '"]'))

        connect()
        assert page.title() == 'Post-processing rules'
        assert page.locator('table').count() == 1
        results['checks'].append('One table loads with local or bearer browser access')
        assert page.evaluate('() => localStorage.length === 0 && sessionStorage.length === 0')
        expect(page.locator('#status')).to_contain_text('updated_at')
        if before:
            # A real valid preview with a comment-only change; never activate in default mode.
            upload({filename: candidate[filename] + '\n// read-only browser preview\n'}, [filename])
            assert active() == before
            page.locator('#cancel-preview').click()
        else:
            upload(candidate)
            assert active() is None, 'Preview must not activate rules.'
            expect(page.locator('#rule-rows > tr')).to_have_count(3)
            expect(page.locator('#rule-rows')).to_contain_text('On activation')
            results['checks'].append('Two schemas and three rules preview without saving')
            if arguments.isolated:
                activate()
            else:
                page.locator('#cancel-preview').click()
        if arguments.isolated:
            initial = active()
            assert initial['files'] == candidate
            expect(page.locator('#rule-rows > tr')).to_have_count(3)
            for key in ['name', 'stage', 'added_at', 'extension', 'order']:
                page.locator('[data-sort="' + key + '"]').click()
                expect(page.locator('[data-sort="' + key + '"]').locator('..')).to_have_attribute('aria-sort', 'ascending')
            orders = row('ad_users.cypher').locator('td').nth(1).inner_text()
            assert orders == '1'
            page.locator('#extension-filter').select_option('cyberark')
            expect(page.locator('#rule-rows > tr')).to_have_count(2)
            page.locator('#select-all').check()
            expect(page.locator('#selected-count')).to_have_text('2 selected')
            for checkbox in page.locator('[data-select-rule]').all():
                expect(checkbox).to_be_checked()
            page.locator('#select-all').uncheck()
            for checkbox in page.locator('[data-select-rule]').all():
                expect(checkbox).not_to_be_checked()
            page.locator('#select-all').check()
            # Exercise each bulk button with two selected rules through the real service.
            bulk_names = ['cyberark_accounts.cypher', 'cyberark_links.cypher']
            page.locator('#disable-selected').click()
            for name in bulk_names:
                expect(page.locator('#confirm-names')).to_contain_text(name)
            assert active() == initial
            page.locator('#confirm-change').click()
            expect(page.locator('#message')).to_contain_text('activated')
            bulk_disabled = active()
            assert sorted(bulk_disabled['disabled']) == bulk_names
            assert bulk_disabled['files'] == initial['files']
            assert bulk_disabled['added_at'] == initial['added_at']
            assert bulk_disabled.get('retired_relationships', []) == initial.get('retired_relationships', [])
            page.locator('#select-all').check()
            for checkbox in page.locator('[data-select-rule]').all():
                expect(checkbox).to_be_checked()
            page.locator('#enable-selected').click()
            for name in bulk_names:
                expect(page.locator('#confirm-names')).to_contain_text(name)
            page.locator('#confirm-change').click()
            expect(page.locator('#message')).to_contain_text('activated')
            bulk_enabled = active()
            assert not bulk_enabled.get('disabled')
            assert bulk_enabled['files'] == initial['files']
            assert bulk_enabled['added_at'] == initial['added_at']
            page.locator('#select-all').check()
            page.locator('#delete-selected').click()
            for name in bulk_names:
                expect(page.locator('#confirm-names')).to_contain_text(name)
            expect(page.locator('#confirm-description')).to_contain_text('next successful analysis')
            assert active() == bulk_enabled
            page.locator('#cancel-change').click()
            assert active() == bulk_enabled
            results['checks'].append('Bulk disable and enable save both selected rules without changing files, dates, or results history; bulk delete cancellation saves nothing')
            initial = bulk_enabled
            page.locator('#select-all').check()
            page.locator('#extension-filter').select_option('active_directory')
            expect(page.locator('#selected-count')).to_have_text('')
            for checkbox in page.locator('[data-select-rule]').all():
                expect(checkbox).not_to_be_checked()
            page.locator('#extension-filter').select_option('')
            results['checks'].append('Sortable execution order and extension-scoped selection work')
            row('ad_users.cypher').get_by_role('button', name='Disable', exact=True).click()
            expect(page.locator('#confirm-paused')).to_contain_text('cyberark_links.cypher')
            assert active() == initial
            page.locator('#confirm-change').click()
            expect(page.locator('#message')).to_contain_text('activated')
            disabled = active()
            assert disabled['disabled'] == ['ad_users.cypher']
            assert disabled['files'] == initial['files'] and disabled.get('retired_relationships', []) == initial.get('retired_relationships', [])
            refreshed()
            expect(row('ad_users.cypher')).to_contain_text('Disabled')
            expect(row('cyberark_links.cypher')).to_contain_text('ad_users.cypher')
            row('ad_users.cypher').get_by_role('button', name='Enable', exact=True).click()
            page.locator('#confirm-change').click()
            expect(page.locator('#message')).to_contain_text('activated')
            assert not active().get('disabled')
            connect()
            assert active()['added_at'] == initial['added_at']
            expect(row('cyberark_links.cypher')).to_contain_text('Enabled')
            results['checks'].append('Disable pauses cross-schema consumers, preserves files/history, and enable persists after page reload')
            # Every changed existing file requires its own explicit overwrite choice.
            replacement = {name: candidate[name] + '\n// overwrite browser check\n' for name in ['ad_users.cypher', 'cyberark_accounts.cypher']}
            upload(replacement, list(replacement))
            assert active()['files'] == initial['files']
            activate()
            updated = active()
            assert updated['added_at'] == initial['added_at']
            for name, contents in replacement.items():
                assert updated['files'][name] == contents
            results['checks'].append('Each overwrite is reviewed and original dates survive activation')
            upload({'ad_users.cypher': replacement['ad_users.cypher'] + '// stale candidate\n'}, ['ad_users.cypher'])
            other_change = {'base_revision': updated['revision'], 'files': {'ad_users.cypher': replacement['ad_users.cypher'] + '// newer revision\n'}, 'overwrite': ['ad_users.cypher']}
            other_review = api('preview', 'POST', other_change)
            newer = api('rules', 'PUT', {**other_change, 'confirm_revision': other_review['revision']})
            page.locator('#activate').click()
            expect(page.locator('#message')).to_contain_text('Refresh and review your changes again')
            assert active() == newer
            refreshed()
            results['checks'].append('A stale browser revision cannot overwrite a newer saved revision')
            row('ad_users.cypher').get_by_role('button', name='Delete ad_users.cypher', exact=True).click()
            expect(page.locator('#confirm-names')).to_contain_text('ad_users.cypher')
            expect(page.locator('#confirm-names')).to_contain_text('cyberark_links.cypher')
            expect(page.locator('#confirm-description')).to_contain_text('next successful analysis')
            page.locator('#cancel-change').click()
            assert active() == newer
            row('ad_users.cypher').get_by_role('button', name='Delete ad_users.cypher', exact=True).click()
            page.locator('#confirm-change').click()
            expect(page.locator('#message')).to_contain_text('activated')
            remaining = active()
            assert 'ad_users.cypher' not in remaining['files'] and 'cyberark_links.cypher' not in remaining['files']
            assert 'cyberark_accounts.cypher' in remaining['files']
            assert 'PoC_CyberArk_AccountFor' in remaining['retired_relationships']
            refreshed()
            expect(page.locator('#rule-rows > tr')).to_have_count(1)
            results['checks'].append('Trash cancellation leaves state unchanged; confirmed cascade retires owned connections and preserves independent rule')
        else:
            assert active() == before, 'Read-only verification must leave saved state unchanged.'
            results['checks'].append('Valid preview discarded without changing saved state')
        page.screenshot(path=str(directory / 'upload-page.png'), full_page=True)
        assert not results['browser_errors'], results['browser_errors']
        browser.close()
    (directory / 'verification.json').write_text(json.dumps(results, indent=2) + '\n')
    print('Rule table browser checks passed' + (' on the isolated service.' if arguments.isolated else '; saved rules unchanged.'))


if __name__ == '__main__':
    main()
