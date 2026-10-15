#!/usr/bin/env python3
"""Native service lifecycle test on Windows/Linux (Python 3 + PyYAML).

Uses disposable files, loopback listeners and generated credentials only. Does
not execute security tools or contact a real model. Logs remain in memory.
"""
import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import ssl
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import yaml


class ModelFixture(BaseHTTPRequestHandler):
    """Return deterministic chat and embeddings without external traffic."""

    def log_message(self, *args):
        pass

    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        if self.path.endswith('/embeddings'):
            inputs = request.get('input', [])
            if not isinstance(inputs, list):
                inputs = [inputs]
            result = {'object': 'list', 'model': 'fixture', 'data': [
                {'object': 'embedding', 'index': index, 'embedding': [0.1, 0.2, 0.3]}
                for index, _ in enumerate(inputs)],
                'usage': {'prompt_tokens': 1, 'total_tokens': 1}}
        elif self.path.endswith('/chat/completions'):
            result = {'id': 'fixture-chat', 'object': 'chat.completion', 'created': 1,
                      'model': 'fixture', 'choices': [{'index': 0, 'finish_reason': 'stop',
                      'message': {'role': 'assistant', 'content': 'Native chat fixture OK.'}}],
                      'usage': {'prompt_tokens': 5, 'completion_tokens': 5, 'total_tokens': 10}}
            if request.get('stream'):
                chunk = dict(result, object='chat.completion.chunk')
                chunk['choices'] = [{'index': 0, 'finish_reason': None,
                    'delta': {'role': 'assistant', 'content': 'Native chat fixture OK.'}}]
                end = dict(result, object='chat.completion.chunk')
                end['choices'] = [{'index': 0, 'finish_reason': 'stop', 'delta': {}}]
                body = ('data: ' + json.dumps(chunk) + '\n\ndata: ' + json.dumps(end) +
                        '\n\ndata: [DONE]\n\n').encode()
                self.send_response(200)
                self.send_header('Content-Type', 'text/event-stream')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
        else:
            self.send_error(400)
            return
        body = json.dumps(result).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class NativeService:
    """Own only this test's child process; surface errors without private logs."""

    def __init__(self, binary, root, port, https=False):
        self.binary, self.root = binary, root
        self.https = https
        self.url = ('https' if https else 'http') + '://127.0.0.1:%d' % port
        self.process = None
        self.token = None
        self.password = None
        self.logs = []
        # Trust only this fixture's self-signed loopback listener; never used for
        # production or arbitrary URLs. The fixture owns every requested path.
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
            urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))

    def request(self, path, data=None, method=None, expected=200, authenticated=True):
        headers = {'Content-Type': 'application/json'}
        if authenticated and self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        request = urllib.request.Request(self.url + path,
            data=None if data is None else json.dumps(data).encode(),
            headers=headers, method=method)
        try:
            response = self.opener.open(request, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            assert response.code == expected, '%s: expected %d, got %d' % (path, expected, response.code)
            body = response.read()
            return json.loads(body) if 'application/json' in response.headers.get('Content-Type', '') else body

    def start(self):
        self.logs = []
        flags = subprocess.CREATE_NEW_PROCESS_GROUP if os.name == 'nt' else 0
        self.process = subprocess.Popen([str(self.binary), '-config', 'config.yaml',
            '--https' if self.https else '--http'],
            cwd=self.root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True, encoding='utf-8', errors='replace', creationflags=flags)
        self.reader = threading.Thread(target=lambda: self.logs.extend(self.process.stdout), daemon=True)
        self.reader.start()
        for _ in range(120):
            if self.process.poll() is not None:
                raise RuntimeError('Service exited before readiness (exit %d)' % self.process.returncode)
            try:
                self.request('/', authenticated=False)
                break
            except (OSError, urllib.error.URLError):
                time.sleep(0.5)
        else:
            raise RuntimeError('Service did not become ready')
        if self.password is None:
            logs = re.sub(r'\x1b\[[0-9;]*m', '', ''.join(self.logs))
            match = re.search(r'Password\s+(\S+)', logs)
            assert match, 'Generated bootstrap credential missing'
            self.password = match.group(1)
        self.token = self.request('/api/auth/login', {
            'username': 'admin', 'password': self.password}, authenticated=False)['token']

    def stop(self):
        if self.process is None:
            return
        if self.process.poll() is None:
            self.process.send_signal(signal.CTRL_BREAK_EVENT if os.name == 'nt' else signal.SIGTERM)
            try:
                code = self.process.wait(timeout=45)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=10)
                raise AssertionError('Graceful service shutdown timed out') from None
            assert code == 0, 'Graceful shutdown failed (exit %d)' % code
        self.reader.join(timeout=5)
        self.process.stdout.close()
        self.process = None
        self.token = None


def run(binary, repository, https=False):
    """Exercise real auth, settings and CRUD across a clean service restart."""
    with tempfile.TemporaryDirectory(prefix='csai native 中文 ') as directory:
        root = Path(directory)
        invalid = root / 'invalid.yaml'
        invalid.write_text('server: [invalid', encoding='utf-8')
        failed = subprocess.run([str(binary), '-config', str(invalid)], cwd=root,
                                capture_output=True, timeout=20)
        assert failed.returncode != 0, 'Invalid config must return a failure exit code'
        invalid.unlink()
        for name in ['web', 'agents', 'roles']:
            shutil.copytree(repository / name, root / name)
        for name in ['tools', 'knowledge_base', 'skills']:
            (root / name).mkdir()
        fixture = ThreadingHTTPServer(('127.0.0.1', 0), ModelFixture)
        thread = threading.Thread(target=fixture.serve_forever, daemon=True)
        thread.start()
        with socket.socket() as reservation:
            reservation.bind(('127.0.0.1', 0))
            port = reservation.getsockname()[1]
        cfg = yaml.safe_load((repository / 'config.example.yaml').read_text(encoding='utf-8'))
        cfg['server'].update(host='127.0.0.1', port=port)
        cfg['c2'] = {'enabled': False}
        cfg['mcp']['enabled'] = False
        cfg['external_mcp'] = {'servers': {}}
        cfg['security']['tools_dir'] = 'tools'
        cfg['security']['process_isolation'] = {'mode': 'auto'}
        cfg['database'] = {'path': 'data/conversations.db', 'knowledge_db_path': 'data/knowledge.db'}
        endpoint = 'http://127.0.0.1:%d/v1' % fixture.server_port
        for channel in cfg['ai']['channels'].values():
            channel.update(base_url=endpoint, api_key='fixture-only', model='fixture')
        cfg['knowledge']['enabled'] = True
        cfg['knowledge']['embedding'].update(base_url=endpoint, api_key='fixture-only', model='fixture')
        (root / 'config.yaml').write_text(yaml.safe_dump(cfg, allow_unicode=True), encoding='utf-8')
        service = NativeService(binary, root, port, https=https)
        try:
            service.start()
            for path in ['/api/config', '/api/conversations', '/api/projects', '/api/assets',
                         '/api/vulnerabilities', '/api/knowledge/items']:
                service.request(path, authenticated=False, expected=401)
                service.request(path)
            service.request('/api/projects', {}, expected=400)
            service.request('/api/assets/import', {'assets': []}, expected=400)
            service.request('/api/knowledge/items', {}, expected=400)
            project = service.request('/api/projects', {'name': '原生兼容 fixture'})
            conversation = service.request('/api/conversations', {
                'title': 'native persistence fixture', 'project_id': project['id']})
            reply = service.request('/api/eino-agent', {
                'conversationId': conversation['id'],
                'message': 'Say hello only. Do not execute any tools.'})
            assert 'Native chat fixture OK.' in reply['response'], 'Chat fixture response missing'
            service.request('/api/assets/import', {'assets': [{
                'host': 'fixture.invalid', 'port': 443, 'protocol': 'https',
                'project_id': project['id'], 'title': 'native fixture'}]})
            vulnerability = service.request('/api/vulnerabilities', {
                'conversation_id': conversation['id'], 'project_id': project['id'],
                'title': 'synthetic persistence fixture', 'severity': 'low',
                'description': 'Local test record; no target scanning.'})
            item = service.request('/api/knowledge/items', {
                'category': '兼容测试', 'title': '原生 fixture', 'content': 'Persistence fixture only.'})
            service.request('/api/config', {'agent': {'max_iterations': 41}}, method='PUT')
            # Wait for the actual embedding row, not only an optimistic UI state.
            for _ in range(60):
                with closing(sqlite3.connect(root / 'data/knowledge.db')) as database:
                    indexed = database.execute('SELECT count(*) FROM knowledge_embeddings').fetchone()[0]
                if indexed:
                    break
                time.sleep(0.25)
            assert indexed, 'Knowledge embedding was not persisted'
            service.stop()
            service.start()
            assert service.request('/api/config')['agent']['max_iterations'] == 41
            assert service.request('/api/conversations/' + conversation['id'])['title'] == conversation['title']
            history = service.request('/api/conversations/' + conversation['id'])
            assert 'Native chat fixture OK.' in json.dumps(history), 'Chat response was not retained'
            assert service.request('/api/projects/' + project['id'])['name'] == project['name']
            assert service.request('/api/vulnerabilities/' + vulnerability['id'])['title'] == vulnerability['title']
            assert service.request('/api/knowledge/items/' + item['id'])['title'] == item['title']
            service.stop()
            for filename in ['conversations.db', 'knowledge.db']:
                with closing(sqlite3.connect(root / 'data' / filename)) as database:
                    assert database.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
            with closing(sqlite3.connect(root / 'data/conversations.db')) as database:
                assert database.execute("SELECT count(*) FROM assets WHERE host=?", ('fixture.invalid',)).fetchone()[0] == 1
            print('PASS (%s): native startup, auth, validation, settings, chat/project/conversation/asset/vulnerability/knowledge persistence, embedding and graceful restart' % ('HTTPS' if https else 'HTTP'))
        finally:
            service.stop()
            fixture.shutdown()
            fixture.server_close()
            thread.join(timeout=5)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--https', action='store_true', help='Exercise the local self-signed HTTPS startup')
    parser.add_argument('--repository', type=Path, default=Path(__file__).resolve().parents[2])
    options = parser.parse_args()
    run(options.binary.resolve(strict=True), options.repository.resolve(strict=True), options.https)
