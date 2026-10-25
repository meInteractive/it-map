#!/usr/bin/env python3
"""Mandatory CI-only real Go/SPA/MariaDB native release/rollback smoke."""
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from publish_release import package
from receive_release import Receiver, WORKFLOW, REPOSITORY, health


class ProcessService:
    def __init__(self, root, address):
        self.root, self.address, self.process = root, address, None

    def stop(self):
        if self.process:
            self.process.terminate()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
            self.process = None

    def restart(self):
        self.stop()
        env = {**os.environ, 'HTTP_ADDR': self.address, 'WEB_DIST_DIR': str(self.root / 'current/web')}
        self.process = subprocess.Popen([str(self.root / 'current/itmap')], env=env,
                                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def launch(self, sequence):
        # The isolated harness invokes apply explicitly; production uses systemd.
        pass


def main():
    if (os.environ.get('GITHUB_ACTIONS') != 'true' or os.environ.get('DB_NAME') != 'itmap_ci'
            or os.environ.get('DB_USER') != 'itmap_readonly_ci' or os.environ.get('DB_HOST') != '127.0.0.1'
            or os.environ.get('APP_ENV') != 'staging'):
        raise RuntimeError('native smoke requires the isolated readonly CI target')
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    with tempfile.TemporaryDirectory(prefix='itmap-native-smoke-') as directory:
        root = Path(directory)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        address = f'127.0.0.1:{port}'
        url = 'http://' + address
        service = ProcessService(root / 'dev', address)
        receiver = Receiver('dev', root / 'dev', root / 'state', service, lambda: health(url))
        identity = dict(repository=REPOSITORY, workflow=WORKFLOW, ref='refs/heads/codex/ci-native',
                        sha=sha, sequence=1, run_id=1)
        try:
            receiver.register(identity)
            artifact = root / 'good.tar'
            info = package(identity, '/tmp/itmap-mariadb', 'dist/web', artifact)
            receiver.upload({**identity, **info}, io.BytesIO(artifact.read_bytes()))
            assert receiver.apply()['result'] == 'success', 'real Go release failed'
            check_login(url)
            previous = receiver.current()
            # Valid ELF header and manifest, invalid executable: the real OS
            # refuses startup and the prior Go process must be restored.
            bad_binary = root / 'bad-binary'
            bad_binary.write_bytes(Path('/tmp/itmap-mariadb').read_bytes()[:64])
            second = {**identity, 'sequence':2}
            receiver.register(second)
            bad = root / 'bad.tar'
            info = package(second, bad_binary, 'dist/web', bad)
            receiver.upload({**second, **info}, io.BytesIO(bad.read_bytes()))
            state = receiver.apply()
            assert state['result'] == 'failed_rolled_back', 'failed release was not rolled back'
            assert receiver.current() == previous, 'previous release pointer lost'
            health(url)
            check_login(url)
            print('::notice::PASS: native real Go/SPA/readonly MariaDB publish, authenticated SQL/API and failed-exec rollback')
        finally:
            service.stop()


def check_login(url):
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    assert b'root' in opener.open(url + '/', timeout=5).read(), 'SPA unavailable'
    body = urllib.parse.urlencode({'login':'eugene','password':os.environ['SITE_PASSWORD']}).encode()
    try:
        opener.open(urllib.request.Request(url+'/auth', data=body, headers={'Origin':url,'Content-Type':'application/x-www-form-urlencoded'}), timeout=5)
        raise AssertionError('login must redirect')
    except urllib.error.HTTPError as response:
        assert response.code == 303, 'login did not succeed'
        cookie = response.headers['Set-Cookie'].split(';')[0]
    with opener.open(urllib.request.Request(url+'/api/session', headers={'Cookie':cookie}), timeout=5) as response:
        assert json.load(response)['name'] == 'Eugene', 'session mismatch'
    with opener.open(urllib.request.Request(url+'/api/companies/list', headers={'Cookie':cookie}), timeout=5) as response:
        companies = json.load(response)['companies']
        assert len(companies) == 1 and companies[0]['id'] == 1 and companies[0]['name'] == 'CI Synthetic Employer', 'native SQL/API fixture mismatch'


if __name__ == '__main__':
    main()
