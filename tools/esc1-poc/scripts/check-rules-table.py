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
"""Exercise the single rules table against an isolated in-memory API fixture."""
import argparse
import copy
import json
import os
from pathlib import Path
from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--browser')
    parser.add_argument('--library-path')
    args = parser.parse_args()
    environment = dict(os.environ)
    if args.library_path:
        environment['LD_LIBRARY_PATH'] = args.library_path
    names = ['PoC_AD/seed.cypher', 'PoC_AD/consumer.cypher', 'PoC_Vault/vault.cypher']
    extensions = [{'name': 'active_directory', 'display_name': 'Active Directory'}, {'name': 'cyberark', 'display_name': 'A CyberArk'}]
    rules = []
    for index, name in enumerate(names):
        rules.append({'name': name, 'order': index + 1, 'stage': 2 if index == 1 else 1,
            'extension': extensions[index // 2]['name'], 'extension_name': extensions[index // 2]['display_name'],
            'added_at': '2026-10-11T12:00:00Z' if index == 0 else '', 'enabled': True, 'disabled_reason': '',
            'namespace': name.split('/')[0], 'version': 'v1.0.0', 'schema_version': 'v1.0.0', 'parameter': name.split('/')[-1][:-7], 'text': '// namespace: ' + name.split('/')[0] + '\n// version: v1.0.0\n' + "RETURN '<script>window.injected=true</script>'", 'inputs': [],
            'outputs': [], 'prerequisites': [{'name': names[0], 'parameter': 'seed'}] if index == 1 else [],
            'dependents': [names[1]] if index == 0 else [], 'processing': {}})
    state = {'revision': 'first', 'files': {name: rule['text'] for name, rule in zip(names, rules)}, 'disabled': []}
    writes, changes, errors = [], [], []
    draft = {'revision': '', 'files': {}, 'ready': False}
    stale_activation = False
    analysis_requests = []
    native_status = {'available': True, 'pending': False, 'running': False, 'native_status': 'Idle', 'message': ''}
    native_error = False
    request_error = False
    current_rules = copy.deepcopy(rules)

    def plan(revision, proposed=None):
        return {'revision': revision, 'queries': proposed if proposed is not None else current_rules, 'extensions': extensions, 'threads': []}

    def check_action_alignment(page):
        # Check rendered geometry, including Enable and Disable labels in the same table.
        action_pairs = page.locator('.rule-actions').evaluate_all('''wrappers => wrappers.map(wrapper =>
            [...wrapper.querySelectorAll('button')].map(button => {
                const bounds = button.getBoundingClientRect();
                return {x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height};
            }))''')
        assert action_pairs
        for toggle, delete in action_pairs:
            assert toggle['y'] == delete['y']
            assert toggle['height'] == delete['height'] == 38
            assert toggle['x'] == action_pairs[0][0]['x']
            assert delete['x'] == action_pairs[0][1]['x']

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(executable_path=args.browser, env=environment)
        page = browser.new_page(viewport={'width': 1440, 'height': 1000})
        page.add_init_script('const originalInterval = window.setInterval; window.setInterval = (callback, delay) => { window.testStatusPoll = callback; return originalInterval(callback, delay); };')
        page.on('pageerror', lambda error: errors.append(str(error)))

        def route(request):
            nonlocal current_rules, stale_activation, draft
            path = request.request.url.split('/')[-1]
            method = request.request.method
            if path == 'browser-mode':
                value = {'local_ui': True}
            elif path == 'analysis':
                if method == 'POST':
                    analysis_requests.append(request.request.post_data_json)
                    if request_error:
                        request.fulfill(status=503, content_type='application/json', body=json.dumps({'error': 'BloodHound response lost'}))
                        return
                    native_status.update(pending=True, message='Analysis requested. BloodHound will run its normal analysis, then check the enabled extension rules.')
                    request.fulfill(status=202, content_type='application/json', body=json.dumps(native_status))
                    return
                if native_error:
                    request.fulfill(status=503, content_type='application/json', body=json.dumps({'error': 'BloodHound is disconnected'}))
                    return
                value = native_status
            elif path == 'status':
                value = {'state': 'running', 'revision': state['revision']}
            elif path == 'plan':
                value = plan(state['revision'])
            elif path == 'draft':
                if method == 'PUT':
                    change = request.request.post_data_json
                    draft['files'].update(change['files'])
                    draft.update(revision='draft', ready=True, overwrite=change.get('overwrite', []))
                elif method == 'DELETE':
                    draft = {'revision': '', 'files': {}, 'ready': False}
                    request.fulfill(status=204)
                    return
                value = draft
            elif path == 'preview':
                change = request.request.post_data_json
                if change.get('draft_revision'):
                    change = {**change, 'files': draft['files'], 'overwrite': draft.get('overwrite', [])}
                changes.append(change)
                proposed = copy.deepcopy(current_rules)
                removed = []
                if change.get('remove'):
                    removed = [names[0], names[1]] if names[0] in change['remove'] else change['remove']
                    proposed = [rule for rule in proposed if rule['name'] not in removed]
                if 'PoC_New/new_rule.cypher' in change.get('files', {}):
                    new_rule = copy.deepcopy(rules[0])
                    new_rule.update(name='PoC_New/new_rule.cypher', namespace='PoC_New', extension='new_extension', extension_name='New extension', order=4)
                    proposed.append(new_rule)
                disabled = change.get('disable', [])
                enabled = change.get('enable', [])
                for rule in proposed:
                    if rule['name'] in disabled or (names[0] in disabled and rule['name'] == names[1]):
                        rule['enabled'] = False
                        rule['disabled_reason'] = 'Disabled by you.' if rule['name'] in disabled else 'Paused because PoC_AD/seed.cypher is disabled.'
                    if rule['name'] in enabled:
                        rule['enabled'] = True
                        rule['disabled_reason'] = ''
                value = {'base_revision': state['revision'], 'revision': 'candidate', 'removed': removed, 'disabled': disabled,
                    'enabled': enabled, 'overwritten': change.get('overwrite', []), 'retired_relationships': ['PoC_Link'] if removed else [], 'plan': plan('candidate', proposed)}
                state['review'] = value
            elif path == 'rules' and method == 'PUT':
                if stale_activation:
                    request.fulfill(status=409, content_type='application/json', body=json.dumps({'error': 'active revision changed'}))
                    return
                writes.append(request.request.post_data_json)
                state['revision'] = 'candidate'
                current_rules = state['review']['plan']['queries']
                state['disabled'] = state['review']['disabled']
                value = {key: value for key, value in state.items() if key != 'review'}
            elif path == 'rules':
                value = {key: value for key, value in state.items() if key != 'review'}
            else:
                request.fulfill(status=200, content_type='text/html', body=(ROOT / 'internal/ruleservice/upload.html').read_text())
                return
            request.fulfill(status=200, content_type='application/json', body=json.dumps(value))

        page.route('**/*', route)
        page.goto('http://rules.test/')
        expect(page.locator('#rule-rows > tr')).to_have_count(3)
        check_action_alignment(page)
        expect(page.locator('#analysis-state')).to_contain_text('running')
        expect(page.locator('#run-analysis')).to_be_enabled()
        request_progress = page.evaluate('''() => {
            document.getElementById('run-analysis').click();
            document.getElementById('run-analysis').click();
            return {disabled: document.getElementById('run-analysis').disabled,
                busy: document.getElementById('analysis-request-state').getAttribute('aria-busy'),
                text: document.getElementById('analysis-request-state').textContent};
        }''')
        assert request_progress['disabled'] and request_progress['busy'] == 'true'
        assert 'Requesting' in request_progress['text']
        expect(page.locator('#analysis-request-state')).to_contain_text('waiting for BloodHound')
        expect(page.locator('#run-analysis')).to_be_disabled()
        assert analysis_requests == [{}]
        native_status.update(pending=False, running=True, native_status='Analyzing')
        page.evaluate('async () => { await window.testStatusPoll(); }')
        expect(page.locator('#analysis-request-state')).to_contain_text('Running')
        expect(page.locator('#analysis-request-state')).to_contain_text('Analyzing')
        expect(page.locator('#run-analysis')).to_be_disabled()
        native_status.update(running=False, native_status='Idle', message='')
        page.evaluate('async () => { await window.testStatusPoll(); }')
        expect(page.locator('#run-analysis')).to_be_enabled()
        expect(page.locator('#analysis-state')).to_contain_text('running')
        native_status.update(available=False, message='An administrator must configure the BloodHound connection.')
        page.evaluate('async () => { await window.testStatusPoll(); }')
        expect(page.locator('#run-analysis')).to_be_disabled()
        expect(page.locator('#analysis-request-state')).to_contain_text('administrator must configure')
        native_error = True
        page.locator('#refresh').click()
        expect(page.locator('#message')).to_contain_text('up to date')
        expect(page.locator('#analysis-request-state')).to_contain_text('disconnected')
        expect(page.locator('#run-analysis')).to_be_disabled()
        expect(page.locator('#rule-rows > tr')).to_have_count(3)
        native_error = False
        native_status.update(available=True, message='')
        page.locator('#refresh').click()
        expect(page.locator('#run-analysis')).to_be_enabled()
        request_error = True
        page.locator('#run-analysis').click()
        expect(page.locator('#analysis-request-state')).to_contain_text('It may have been queued')
        expect(page.locator('#run-analysis')).to_be_disabled()
        assert analysis_requests == [{}, {}]
        request_error = False
        page.locator('#refresh').click()
        expect(page.locator('#run-analysis')).to_be_enabled()
        busy_state = page.evaluate("() => { refresh(); return {message: document.getElementById('message').textContent, busy: document.getElementById('message').getAttribute('aria-busy'), disabled: document.getElementById('refresh').disabled}; }" )
        assert busy_state['busy'] == 'true' and busy_state['disabled'] and 'Refreshing' in busy_state['message']
        expect(page.locator('#message')).to_contain_text('up to date')
        checking = page.evaluate("() => { window.testStatusPoll(); return {text: document.getElementById('analysis-state').textContent, busy: document.getElementById('analysis-state').getAttribute('aria-busy')}; }")
        assert checking['busy'] == 'true' and checking['text'] == 'Checking status…'
        expect(page.locator('#analysis-state')).to_contain_text('running')
        assert page.locator('table').count() == 1
        expect(page.locator('#rule-rows').get_by_role('button', name='▸ PoC_AD/seed.cypher')).to_be_visible()
        page.get_by_role('button', name='▸ PoC_AD/seed.cypher').click()
        expect(page.locator('#rule-rows')).to_contain_text("<script>window.injected=true</script>")
        assert page.evaluate('window.injected') is None
        page.get_by_role('button', name='▾ PoC_AD/seed.cypher').click()
        page.get_by_role('button', name='Name', exact=True).click()
        expect(page.locator('#rule-rows > tr').first).to_contain_text('PoC_AD/consumer.cypher')
        page.get_by_role('button', name='Stage', exact=True).click()
        expect(page.locator('#rule-rows > tr').first).to_contain_text('PoC_AD/seed.cypher')
        page.get_by_role('button', name='Date added', exact=True).click()
        expect(page.locator('#rule-rows > tr').first).to_contain_text('PoC_AD/consumer.cypher')
        page.get_by_role('button', name='Extension', exact=True).click()
        expect(page.locator('#rule-rows > tr').first).to_contain_text('PoC_Vault/vault.cypher')
        page.get_by_role('button', name='Stage', exact=True).click()
        page.get_by_role('button', name='Order', exact=True).click()
        expect(page.locator('#rule-rows > tr').first).to_contain_text('PoC_AD/seed.cypher')
        expect(page.locator('#rule-rows > tr').first.locator('td').nth(1)).to_have_text('1')
        page.locator('#extension-filter').select_option('cyberark')
        page.locator('#select-all').check()
        expect(page.locator('#selected-count')).to_have_text('1 selected')
        for checkbox in page.locator('[data-select-rule]').all():
            expect(checkbox).to_be_checked()
        page.locator('#select-all').uncheck()
        for checkbox in page.locator('[data-select-rule]').all():
            expect(checkbox).not_to_be_checked()
        page.locator('#select-all').check()
        page.locator('#extension-filter').select_option('active_directory')
        expect(page.locator('#selected-count')).to_have_text('')
        expect(page.locator('#select-all')).not_to_be_checked()
        for checkbox in page.locator('[data-select-rule]').all():
            expect(checkbox).not_to_be_checked()
        page.get_by_role('button', name='Delete PoC_AD/seed.cypher', exact=True).click()
        expect(page.locator('#confirm-dialog')).to_be_visible()
        expect(page.locator('#confirm-paused')).not_to_be_visible()
        expect(page.locator('#confirm-names')).to_contain_text('PoC_AD/consumer.cypher')
        expect(page.locator('#confirm-description')).to_contain_text('next successful analysis')
        assert not writes
        page.locator('#cancel-change').click()
        expect(page.locator('#confirm-dialog')).not_to_be_visible()
        assert not writes
        # Exercise literal names and reasons in the confirmation without saving a change.
        unsafe_name = '<img src=x onerror="window.injected=true">.cypher'
        unsafe_reason = '<script>window.injected=true</script>'
        page.evaluate('''review => { void confirmAction('disable', ['PoC_AD/seed.cypher'], review); }''',
            {'plan': {'queries': [{'name': unsafe_name, 'disabled_reason': unsafe_reason}]}})
        expect(page.locator('#confirm-paused-rules li')).to_have_count(1)
        expect(page.locator('#confirm-paused-rules strong')).to_have_text(unsafe_name)
        expect(page.locator('#confirm-paused-rules .muted')).to_have_text(unsafe_reason)
        assert page.locator('#confirm-paused img, #confirm-paused script').count() == 0
        assert page.evaluate('window.injected') is None
        page.locator('#cancel-change').click()
        page.locator('#rule-rows > tr').first.get_by_role('button', name='Disable', exact=True).click()
        expect(page.locator('#confirm-paused-rules li')).to_have_count(2)
        expect(page.locator('#confirm-paused-rules li').first.locator('strong')).to_have_text('PoC_AD/seed.cypher')
        expect(page.locator('#confirm-paused-rules li').first.locator('.muted')).to_have_text('Disabled by you.')
        expect(page.locator('#confirm-paused-rules li').nth(1).locator('strong')).to_have_text('PoC_AD/consumer.cypher')
        expect(page.locator('#confirm-paused-rules li').nth(1).locator('.muted')).to_have_text('Paused because PoC_AD/seed.cypher is disabled.')
        expect(page.locator('#confirm-description')).to_contain_text('existing graph results will stay')
        page.locator('#confirm-change').click()
        expect(page.locator('#message')).to_contain_text('activated')
        assert len(writes) == 1 and writes[0]['disable'] == ['PoC_AD/seed.cypher']
        expect(page.locator('#rule-rows')).to_contain_text('Paused because PoC_AD/seed.cypher is disabled.')
        check_action_alignment(page)
        page.locator('#rule-rows > tr').first.get_by_role('button', name='Enable', exact=True).click()
        expect(page.locator('#confirm-title')).to_have_text('Enable these rules?')
        page.locator('#cancel-change').click()
        page.locator('#upload-controls > summary').click()
        page.locator('#files').set_input_files([
            {'name': 'new_rule.cypher', 'mimeType': 'text/plain', 'buffer': b'// namespace: PoC_New\n// version: v1.0.0\nRETURN 42'}
        ])
        page.locator('#preview').click()
        expect(page.locator('#review')).to_be_visible()
        expect(page.locator('#run-analysis')).to_be_disabled()
        page.locator('#extension-filter').select_option('new_extension')
        expect(page.locator('#rule-rows > tr')).to_have_count(1)
        page.locator('#cancel-preview').click()
        expect(page.locator('#extension-filter')).to_have_value('')
        assert page.locator('#extension-filter option[value="new_extension"]').count() == 0
        expect(page.locator('#rule-rows > tr')).to_have_count(3)
        page.locator('#files').set_input_files([
            {'name': name.split('/')[-1], 'mimeType': 'text/plain', 'buffer': b'// namespace: PoC_AD\n// version: v1.0.0\nRETURN 42'} for name in names[:2]
        ])
        page.locator('#preview').click()
        expect(page.locator('#overwrite-name')).to_have_text(names[0])
        page.locator('#overwrite-file').click()
        expect(page.locator('#overwrite-name')).to_have_text(names[1])
        page.locator('#overwrite-file').click()
        expect(page.locator('#review')).to_be_visible()
        assert changes[-1]['overwrite'] == names[:2]
        stale_activation = True
        page.locator('#activate').click()
        expect(page.locator('#message')).to_contain_text('Refresh and review your changes again')
        expect(page.locator('#review')).not_to_be_visible()
        assert len(writes) == 1
        assert not errors, errors
        browser.close()
    print('Single table, sorting, filtering, safe details and confirmation lists, aligned actions, scoped selection, cascade confirmation, disable and enable, and native analysis request/status checks passed.')


if __name__ == '__main__':
    main()
