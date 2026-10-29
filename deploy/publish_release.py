#!/usr/bin/env python3
"""GitHub push context, deterministic native package and bounded SSH client."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request

from receive_release import MAX_ARCHIVE, MAX_CONTENT, MAX_FILES, REPOSITORY, WORKFLOW, Rejected, request_identity


def event_request(event, payload, env):
    ref = env.get('GITHUB_REF', '')
    operation = 'register'
    if event not in ('push', 'workflow_dispatch'):
        raise Rejected('unsupported_event')
    if event == 'push' and payload.get('ref') != ref:
        raise Rejected('event_ref_mismatch')
    # На этапе A разрешён только main; будущий ref спринта1.2 ещё не задан.
    if ref != 'refs/heads/main':
        raise Rejected('only_main_deployment_supported')
    slot = 'main'
    try:
        request = dict(repository=env.get('GITHUB_REPOSITORY'), workflow=WORKFLOW,
                       ref=ref, sha=env.get('GITHUB_SHA'), sequence=int(env.get('GITHUB_RUN_NUMBER', '')),
                       run_id=int(env.get('GITHUB_RUN_ID', '')))
    except ValueError:
        raise Rejected('invalid_run_metadata') from None
    request_identity(request, slot)
    return slot, operation, request


def package(request, binary, web, output, verify_checkout=True):
    slot = 'main' if request['ref'] == 'refs/heads/main' else 'dev'
    request_identity(request, slot)
    if verify_checkout:
        sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
        if sha != request['sha'] or subprocess.run(['git', 'diff', '--quiet', 'HEAD', '--']).returncode:
            raise Rejected('checkout_sha_or_content_mismatch')
    binary, web, output = Path(binary), Path(web), Path(output)
    if binary.is_symlink() or not binary.is_file() or web.is_symlink() or not web.is_dir():
        raise Rejected('invalid_package_inputs')
    paths = {'itmap': binary}
    for path in sorted(web.rglob('*')):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise Rejected('unsafe_package_input')
        if path.is_file():
            paths['web/' + path.relative_to(web).as_posix()] = path
    if 'web/index.html' not in paths or len(paths) + 1 > MAX_FILES:
        raise Rejected('incomplete_or_large_package')
    if any(p.stat().st_size > 32 * 1024 * 1024 for p in paths.values()) or sum(p.stat().st_size for p in paths.values()) > min(MAX_CONTENT, MAX_ARCHIVE - 1024 * 1024):
        raise Rejected('package_size_limit')
    manifest = {'identity': request, 'files': {k: hashlib.sha256(p.read_bytes()).hexdigest() for k, p in paths.items()}}
    with tarfile.open(output, 'w') as tar:
        for name, data in [(name, p.read_bytes()) for name, p in sorted(paths.items())] + [('manifest.json', json.dumps(manifest, sort_keys=True).encode())]:
            entry = tarfile.TarInfo(name)
            entry.size = len(data)
            entry.mode = 0o755 if name == 'itmap' else 0o644
            entry.mtime = 0
            tar.addfile(entry, io.BytesIO(data))
    if output.stat().st_size > MAX_ARCHIVE:
        raise Rejected('package_size_limit')
    return {'size': output.stat().st_size, 'digest': hashlib.sha256(output.read_bytes()).hexdigest()}


class Client:
    def __init__(self, env):
        key, known = env.get('DEPLOY_SSH_KEY', ''), env.get('DEPLOY_KNOWN_HOSTS', '')
        host, user = env.get('DEPLOY_HOST', ''), env.get('DEPLOY_USER', '')
        try:
            port = int(env.get('DEPLOY_PORT') or '22')
        except ValueError:
            raise Rejected('invalid_ssh_settings') from None
        if (not key or not known or not 0 < port < 65536 or
                not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.:-]{0,252}', host) or
                not re.fullmatch(r'[a-z_][a-z0-9_-]{0,63}', user)):
            raise Rejected('missing_or_invalid_ssh_settings')
        self.temp = tempfile.TemporaryDirectory(prefix='itmap-ssh-')
        root = Path(self.temp.name)
        try:
            for name, value in [('identity', key), ('known_hosts', known)]:
                path = root / name
                path.write_text(value + ('\n' if not value.endswith('\n') else ''))
                path.chmod(0o600)
        except OSError:
            self.temp.cleanup()
            raise Rejected('ssh_key_setup_failed') from None
        self.command = ['ssh', '-F', '/dev/null', '-T', '-p', str(port), '-i', str(root / 'identity'),
                        '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes', '-o', 'StrictHostKeyChecking=yes',
                        '-o', 'GlobalKnownHostsFile=/dev/null', '-o', 'UserKnownHostsFile=' + str(root / 'known_hosts'),
                        '-o', 'ConnectTimeout=15', '-o', 'ServerAliveInterval=15', '-o', 'ServerAliveCountMax=3',
                        '-o', 'LogLevel=ERROR', user + '@' + host, 'receive']
        self.child_env = {k: v for k, v in env.items() if k not in ('DEPLOY_SSH_KEY', 'DEPLOY_KNOWN_HOSTS', 'GH_TOKEN')}

    def close(self):
        self.temp.cleanup()

    def request(self, request, body=b''):
        data = json.dumps(request, ensure_ascii=True).encode() + b'\n' + body
        try:
            result = subprocess.run(self.command, input=data, capture_output=True, timeout=180, env=self.child_env)
            if len(result.stdout) > 8 * 1024 * 1024:
                raise Rejected('oversized_receiver_response')
            response = json.loads(result.stdout)
        except (OSError, subprocess.TimeoutExpired, ValueError):
            raise Rejected('ssh_request_failed') from None
        if not isinstance(response, dict):
            raise Rejected('invalid_receiver_response')
        if 'error' in response:
            code = response['error']
            raise Rejected(code if isinstance(code, str) and re.fullmatch('[a-z_]{1,80}', code) else 'receiver_rejected')
        if result.returncode and response.get('result') not in ('failed_rolled_back', 'rollback_failed'):
            raise Rejected('ssh_request_failed')
        return response

    def publish(self, request, archive, check_head=True):
        if check_head:
            verify_remote_head(request)
        body = Path(archive).read_bytes()
        if not 0 < len(body) <= MAX_ARCHIVE:
            raise Rejected('package_size_limit')
        digest = hashlib.sha256(body).hexdigest()
        deadline = time.monotonic() + 600
        uploaded = False
        while time.monotonic() < deadline:
            state = self.request({'operation': 'status'})
            if state.get('candidate') != request:
                raise Rejected('superseded')
            current = state.get('current') or {}
            pending = state.get('pending') or {}
            if state.get('result') == 'success' and current.get('identity') == request:
                if current.get('digest') != digest:
                    raise Rejected('immutable_release_conflict')
                return state
            if uploaded and state.get('result') in ('failed_rolled_back', 'rollback_failed', 'superseded'):
                raise Rejected(state['result'])
            if state.get('result') in ('queued', 'applying'):
                if pending.get('identity') == request:
                    if pending.get('digest') != digest:
                        raise Rejected('immutable_release_conflict')
                    uploaded = True
            elif not uploaded:
                self.request({**request, 'operation': 'upload', 'size': len(body), 'digest': digest}, body)
                uploaded = True
            time.sleep(3)
        raise Rejected('deployment_timeout_check_server_status')


def verify_remote_head(request):
    branch = urllib.parse.quote(request['ref'][len('refs/heads/'):], safe='')
    url = f'https://api.github.com/repos/{REPOSITORY}/git/ref/heads/{branch}'
    token = os.environ.get('GH_TOKEN', '')
    if not token:
        raise Rejected('missing_github_read_token')
    query = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + token,
                                               'Accept': 'application/vnd.github+json', 'User-Agent': 'itmap-publish-v1'})
    try:
        with urllib.request.urlopen(query, timeout=15) as response:
            head = json.loads(response.read(16384))['object']['sha']
    except Exception:
        raise Rejected('github_ref_check_failed') from None
    if head != request['sha']:
        raise Rejected('branch_head_changed')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=('context', 'package', 'register', 'invalidate', 'publish', 'status'))
    parser.add_argument('--binary')
    parser.add_argument('--web')
    parser.add_argument('--archive')
    args = parser.parse_args()
    if args.operation == 'context':
        payload = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
        slot, operation, request = event_request(os.environ['GITHUB_EVENT_NAME'], payload, os.environ)
        with open(os.environ['GITHUB_OUTPUT'], 'a') as out:
            out.write(f'slot={slot}\noperation={operation}\nrequest={json.dumps(request, ensure_ascii=True)}\n')
        return
    request = json.loads(os.environ.get('REQUEST_JSON', '{}'))
    if args.operation == 'package':
        package(request, args.binary, args.web, args.archive)
        print('PASS: immutable native artifact packaged for the requested SHA')
        return
    client = Client(os.environ)
    try:
        if args.operation == 'publish':
            state = client.publish(request, args.archive)
        else:
            state = client.request({'operation': 'status'} if args.operation == 'status' else {**request, 'operation': args.operation})
        print(json.dumps({'result': state.get('result'), 'sequence': state.get('sequence')}))
    finally:
        client.close()


if __name__ == '__main__':
    try:
        main()
    except Rejected as error:
        print('Publication blocked: ' + str(error), file=sys.stderr)
        sys.exit(1)
    except Exception:
        print('Publication blocked: client_failed', file=sys.stderr)
        sys.exit(1)
