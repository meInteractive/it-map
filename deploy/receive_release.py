#!/usr/bin/env python3
"""Root-owned, forced-SSH native release receiver. No SQL or shell evaluation."""
import argparse
import contextlib
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request

REPOSITORY = 'meInteractive/it-map'
WORKFLOW = 'publish-v1'
MAX_ARCHIVE = 64 * 1024 * 1024
MAX_CONTENT = 128 * 1024 * 1024
MAX_FILES = 2000
SHA = re.compile(r'^[0-9a-f]{40}$')
DIGEST = re.compile(r'^[0-9a-f]{64}$')


class Rejected(Exception):
    """A bounded public error code, never arbitrary exception/request text."""


def atomic_json(path, value):
    data = (json.dumps(value, ensure_ascii=True, sort_keys=True) + '\n').encode()
    fd, name = tempfile.mkstemp(prefix='.write-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as out:
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, path)
        fsync_dir(path.parent)
    finally:
        Path(name).unlink(missing_ok=True)


def fsync_dir(path):
    fd = os.open(path, os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def valid_ref(ref):
    return (isinstance(ref, str) and ref.startswith('refs/heads/') and len(ref) <= 512
            and not any(ord(c) < 33 or c in '~^:?*[\\' for c in ref)
            and not any(s in ref for s in ('..', '//', '@{'))
            and all(p and not p.startswith('.') and not p.endswith(('.', '.lock'))
                    for p in ref.split('/')))


def request_identity(request, slot):
    if (request.get('repository') != REPOSITORY or request.get('workflow') != WORKFLOW
            or not valid_ref(request.get('ref'))
            or not isinstance(request.get('sha'), str) or not SHA.fullmatch(request['sha'])
            or type(request.get('sequence')) is not int or not 0 < request['sequence'] < 2**63
            or type(request.get('run_id')) is not int or not 0 < request['run_id'] < 2**63):
        raise Rejected('invalid_identity')
    if (request['ref'] == 'refs/heads/main') != (slot == 'main'):
        raise Rejected('wrong_slot')
    return {k: request[k] for k in ('repository', 'workflow', 'ref', 'sha', 'sequence', 'run_id')}


class Systemd:
    def __init__(self, slot):
        self.slot = slot

    def restart(self):
        subprocess.run(['/usr/bin/systemctl', 'restart', f'itmap@{self.slot}.service'],
                       check=True, timeout=40, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def stop(self):
        subprocess.run(['/usr/bin/systemctl', 'stop', f'itmap@{self.slot}.service'],
                       check=True, timeout=40, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def verify_executable(self, expected):
        result = subprocess.run(
            ['/usr/bin/systemctl', 'show', f'itmap@{self.slot}.service', '-p', 'MainPID', '--value'],
            check=True, capture_output=True, text=True, timeout=5)
        pid = int(result.stdout.strip())
        if pid <= 1 or Path('/proc', str(pid), 'exe').resolve(strict=True) != expected.resolve(strict=True):
            raise Rejected('api_executable_mismatch')

    def launch(self, sequence):
        unit = f'itmap-release-{self.slot}-{sequence}.service'
        result = subprocess.run([
            '/usr/bin/systemd-run', '--quiet', '--collect', '--unit', unit,
            '--property=Type=exec', '--property=RuntimeMaxSec=300',
            '--property=UMask=0077', sys.executable, '-I',
            '/usr/local/libexec/itmap-receive.py', '--slot', self.slot, '--apply'],
            timeout=20, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if result.returncode:
            active = subprocess.run(['/usr/bin/systemctl', 'is-active', '--quiet', unit],
                                    timeout=10).returncode == 0
            if not active:
                raise Rejected('worker_launch_failed')


class UnixWorkerConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=3)
        self.path = path

    def connect(self):
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            sock.settimeout(self.timeout)
            sock.connect(self.path)
        except OSError:
            sock.close()
            raise
        self.sock = sock


class WorkerDrain:
    """Подтверждение паузы для одного MainPID без сетевого доступа к control."""

    unit = 'itmap-worker.service'
    token_pattern = re.compile(r'[a-zA-Z0-9_-]{1,96}')

    def __init__(self, root=Path('/opt/itmap/worker'), control='/run/itmap-worker/release.sock'):
        self.hold = root / 'release-hold'
        self.control = control

    def main_pid(self):
        result = subprocess.run(
            ['/usr/bin/systemctl', 'show', self.unit, '-p', 'MainPID', '--value'],
            check=True, capture_output=True, text=True, timeout=5)
        pid = int(result.stdout.strip())
        if pid <= 1:
            raise Rejected('worker_instance_unavailable')
        return pid

    def message(self, path, request=None):
        with contextlib.closing(UnixWorkerConnection(self.control)) as conn:
            if request is None:
                conn.request('GET', path)
            else:
                conn.request('POST', path, json.dumps(request), {'Content-Type': 'application/json'})
            with conn.getresponse() as response:
                if response.status != 200:
                    raise Rejected('worker_control_rejected')
                data = response.read(4097)
                if len(data) > 4096:
                    raise Rejected('worker_control_invalid')
                value = json.loads(data)
        if not isinstance(value, dict):
            raise Rejected('worker_control_invalid')
        return value

    def instance(self):
        value = self.message('/instance')
        nonce, pid = value.get('instance'), value.get('pid')
        if (not isinstance(nonce, str) or not re.fullmatch(r'[0-9a-f]{32}', nonce)
                or type(pid) is not int or pid != self.main_pid()
                or type(value.get('paused')) is not bool):
            raise Rejected('worker_instance_changed')
        return value

    def service(self, operation):
        if operation not in ('start', 'stop'):
            raise Rejected('invalid_worker_service_operation')
        subprocess.run(['/usr/bin/systemctl', operation, self.unit], check=True, timeout=95,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def active(self):
        result = subprocess.run(['/usr/bin/systemctl', 'show', self.unit, '-p', 'ActiveState', '--value'],
                                check=True, capture_output=True, text=True, timeout=5)
        return result.stdout.strip() not in ('inactive', 'failed')

    def executable(self, expected, paused=None):
        current = self.instance()
        if Path('/proc', str(current['pid']), 'exe').resolve(strict=True) != expected.resolve(strict=True):
            raise Rejected('worker_executable_mismatch')
        if paused is not None and current['paused'] != paused:
            raise Rejected('worker_pause_state_mismatch')
        return current

    def wait_executable(self, expected, paused=None):
        for attempt in range(15):
            try:
                return self.executable(expected, paused)
            except (OSError, ValueError, http.client.HTTPException, Rejected, subprocess.SubprocessError):
                if attempt == 14:
                    raise Rejected('worker_start_unconfirmed') from None
                time.sleep(1)

    def ready(self):
        with contextlib.closing(http.client.HTTPConnection('127.0.0.1', 18082, timeout=3)) as conn:
            conn.request('GET', '/readyz')
            with conn.getresponse() as response:
                snapshot = json.loads(response.read(4096))
                if response.status != 200 or snapshot.get('ready') is not True or snapshot.get('live') is not True:
                    raise Rejected('worker_readiness_failed')

    def hold_token(self):
        try:
            fd = os.open(self.hold, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        except FileNotFoundError:
            return None
        with os.fdopen(fd, 'r', encoding='ascii') as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o022:
                raise Rejected('unsafe_worker_hold')
            token = stream.read(98).removesuffix('\n')
        if not self.token_pattern.fullmatch(token):
            raise Rejected('unsafe_worker_hold')
        return token

    def create_hold(self, token):
        if not isinstance(token, str) or not self.token_pattern.fullmatch(token):
            raise Rejected('invalid_worker_hold_token')
        fd, name = tempfile.mkstemp(prefix='.worker-hold-', dir=self.hold.parent)
        try:
            with os.fdopen(fd, 'w', encoding='ascii') as stream:
                stream.write(token + '\n')
                stream.flush()
                os.fchmod(stream.fileno(), 0o644)
                os.fsync(stream.fileno())
            try:
                os.link(name, self.hold)
            except FileExistsError:
                if self.hold_token() != token:
                    raise Rejected('worker_hold_conflict') from None
            fsync_dir(self.hold.parent)
        finally:
            Path(name).unlink(missing_ok=True)

    def ledger(self):
        evidence = self.message('/ledger')
        count, digest = evidence.get('count'), evidence.get('digest')
        if type(count) is not int or not 0 <= count <= 1024 or not isinstance(digest, str) or not DIGEST.fullmatch(digest):
            raise Rejected('worker_ledger_invalid')
        return evidence

    def verify_idle(self, baseline):
        # Прямое loopback-чтение, без прокси и без перехода по redirect.
        with contextlib.closing(http.client.HTTPConnection('127.0.0.1', 18082, timeout=3)) as conn:
            snapshots = {}
            for endpoint in ('queuez', 'recoveryz'):
                conn.request('GET', '/' + endpoint)
                with conn.getresponse() as response:
                    if response.status != 200:
                        raise Rejected('worker_diagnostics_unavailable')
                    data = response.read(4097)
                    if len(data) > 4096:
                        raise Rejected('worker_diagnostics_invalid')
                    snapshots[endpoint] = json.loads(data)
        queue, recovery = snapshots['queuez'], snapshots['recoveryz']
        try:
            values = [queue[kind]['running'] for kind in ('scan', 'pipeline')]
            values += [recovery[key] for key in (
                'actorCurrentUnresolved', 'currentAIReservedRunning', 'actorUnknown', 'actorReconcile',
                'schedulerHistoricalUnknown', 'schedulerOrphanUnknown',
                'schedulerHistoricalZeroReserveProviderUnknown', 'schedulerOrphanZeroReserveProviderUnknown')]
            old_ai = [recovery[key] for key in ('currentAIReservedOther', 'currentAIZeroReserveProviderUnknown')]
        except (KeyError, TypeError):
            raise Rejected('worker_diagnostics_invalid') from None
        if any(type(value) is not int or value != 0 for value in values):
            raise Rejected('worker_unsettled_operations')
        if (any(type(value) is not int or value < 0 for value in old_ai)
                or sum(old_ai) != baseline['count'] or self.ledger() != baseline):
            raise Rejected('worker_ledger_changed')

    def drain(self, token, timeout=30):
        original = self.instance()
        baseline = self.ledger()
        self.create_hold(token)
        request = {'instance': original['instance'], 'token': token}
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            state = self.message('/pause', request).get('state')
            if state == 'held':
                self.verify_idle(baseline)
                confirmed = self.instance()
                if (confirmed['instance'] != original['instance'] or confirmed['pid'] != original['pid']
                        or not confirmed['paused'] or self.hold_token() != token):
                    raise Rejected('worker_instance_changed')
                return confirmed
            if state != 'draining':
                raise Rejected('worker_drain_unsafe')
            time.sleep(1)
        raise Rejected('worker_drain_timeout')

    def resume(self, token, expected):
        current = self.instance()
        if current['instance'] != expected['instance'] or current['pid'] != expected['pid']:
            raise Rejected('worker_instance_changed')
        hold = self.hold_token()
        if hold is not None:
            if hold != token:
                raise Rejected('worker_hold_conflict')
            self.hold.unlink()
            fsync_dir(self.hold.parent)
        if self.message('/resume', {'instance': expected['instance'], 'token': token}).get('state') != 'resumed':
            raise Rejected('worker_resume_unconfirmed')
        current = self.instance()
        if current['instance'] != expected['instance'] or current['pid'] != expected['pid'] or current['paused']:
            raise Rejected('worker_resume_unconfirmed')


def health(url):
    # Never consult proxy environment, redirect or external host for health.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    for path, code, expected in [
        ('/health/live', 200, {'status': 'alive'}),
        ('/health/ready', 503, {'status': 'not_ready', 'reason': 'business_api_not_migrated'}),
    ]:
        try:
            response = opener.open(url + path, timeout=2)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            if response.code != code or json.loads(response.read(4096)) != expected:
                raise Rejected('health_failed')
    try:
        response = opener.open(url + '/', timeout=2)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        if (response.code != 200 or not response.headers.get('Content-Type', '').startswith('text/html')
                or b'id="root"' not in response.read(8192)):
            raise Rejected('spa_unavailable')


class Receiver:
    def __init__(self, slot, root=None, state_dir=None, service=None, probe=None, *, worker_root=None, worker=None):
        if slot not in ('main', 'dev'):
            raise Rejected('invalid_slot')
        self.slot = slot
        self.root = Path(root or f'/opt/itmap/{slot}')
        self.state_dir = Path(state_dir or f'/var/lib/itmap-deploy/{slot}')
        self.service = service or Systemd(slot)
        self.probe = probe or (lambda: health('http://127.0.0.1:' + ('18080' if slot == 'main' else '18081')))
        self.worker_root = Path(worker_root) if worker_root is not None else None
        if worker is not None and (slot != 'main' or self.worker_root is None):
            raise Rejected('invalid_joint_release_slot')
        self.worker = worker
        self.state_file = self.state_dir / 'state.json'
        self.root.mkdir(parents=True, exist_ok=True, mode=0o755)
        self.root.chmod(0o755)
        self.state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        (self.root / 'releases').mkdir(exist_ok=True, mode=0o755)
        (self.root / 'releases').chmod(0o755)

    @contextlib.contextmanager
    def locked(self):
        with (self.state_dir / 'lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            yield

    def load(self):
        if not self.state_file.exists():
            return {'version': 1, 'workflow': WORKFLOW, 'sequence': 0, 'candidate': None,
                    'deleted': {}, 'result': 'idle', 'current': None, 'pending': None}
        try:
            state = json.loads(self.state_file.read_text())
        except (OSError, ValueError):
            raise Rejected('state_unreadable') from None
        if state.get('version') != 1 or state.get('workflow') != WORKFLOW:
            raise Rejected('state_version_mismatch')
        return state

    def save(self, state):
        atomic_json(self.state_file, state)

    def register(self, request):
        identity = request_identity(request, self.slot)
        with self.locked():
            state = self.load()
            if state['result'] == 'rollback_failed' and state.get('joint'):
                raise Rejected('joint_rollback_requires_operator')
            seq = identity['sequence']
            if seq <= state['deleted'].get(identity['ref'], 0) or seq < state['sequence']:
                raise Rejected('superseded')
            if seq == state['sequence']:
                if state['result'] == 'rollback_failed' and state.get('joint'):
                    raise Rejected('joint_rollback_requires_operator')
                if identity != state['candidate']:
                    raise Rejected('sequence_conflict')
                return state
            state.update(sequence=seq, candidate=identity)
            if state['result'] not in ('queued', 'applying'):
                state['result'] = 'registered'
            self.save(state)
            return state

    def invalidate(self, request):
        identity = request_identity(request, self.slot)
        with self.locked():
            state = self.load()
            if state['result'] == 'rollback_failed' and state.get('joint'):
                raise Rejected('joint_rollback_requires_operator')
            ref, seq = identity['ref'], identity['sequence']
            state['deleted'][ref] = max(seq, state['deleted'].get(ref, 0))
            if len(state['deleted']) > 10000:
                raise Rejected('tombstone_limit')
            if state['candidate'] and ref == state['candidate']['ref'] and seq >= state['sequence']:
                state.update(sequence=seq, candidate=None)
                if state['result'] not in ('queued', 'applying'):
                    state['result'] = 'deleted'
            self.save(state)
            return state

    def upload(self, request, stream):
        identity = request_identity(request, self.slot)
        joint = request.get('joint', False)
        if type(joint) is not bool or joint != (self.worker is not None):
            raise Rejected('joint_release_policy_mismatch')
        size, digest = request.get('size'), request.get('digest')
        if type(size) is not int or not 0 < size <= MAX_ARCHIVE or not isinstance(digest, str) or not DIGEST.fullmatch(digest):
            raise Rejected('invalid_archive_header')
        # Receive into a private file before taking the activation lock.
        fd, name = tempfile.mkstemp(prefix='incoming-', dir=self.state_dir)
        incoming = Path(name)
        try:
            hasher = hashlib.sha256()
            with os.fdopen(fd, 'wb') as out:
                remaining = size
                while remaining:
                    data = stream.read(min(1024 * 1024, remaining))
                    if not data:
                        raise Rejected('truncated_archive')
                    out.write(data)
                    hasher.update(data)
                    remaining -= len(data)
                if stream.read(1):
                    raise Rejected('trailing_archive_data')
                out.flush()
                os.fsync(out.fileno())
            if hasher.hexdigest() != digest:
                raise Rejected('archive_digest_mismatch')
            with self.locked():
                state = self.load()
                if identity != state['candidate']:
                    raise Rejected('superseded')
                if state.get('pending'):
                    pending = state['pending']
                    if pending['identity'] == identity:
                        if pending['digest'] != digest:
                            raise Rejected('immutable_release_conflict')
                        if state['result'] == 'success' and state['current'] and state['current']['identity'] == identity:
                            return state
                        if state['result'] in ('queued', 'applying'):
                            self.service.launch(identity['sequence'])
                            return state
                    elif state['result'] in ('queued', 'applying'):
                        raise Rejected('previous_worker_pending')
                release = f"{identity['sequence']}-{identity['sha']}"
                archive = self.state_dir / (release + '.tar')
                os.replace(incoming, archive)
                fsync_dir(self.state_dir)
                state.pop('defer_reason', None)
                state.update(result='queued', pending={'identity': identity, 'digest': digest,
                                                      'archive': archive.name, 'release': release, 'joint': joint})
                self.save(state)
                self.service.launch(identity['sequence'])
                return state
        finally:
            incoming.unlink(missing_ok=True)

    def unpack(self, archive, destination, pending):
        expected_machine = {'x86_64': 62, 'aarch64': 183}.get(platform.machine())
        binaries = {'itmap', 'itmap-worker'} if pending.get('joint') else {'itmap'}
        with tarfile.open(archive, 'r:') as tar:
            members = tar.getmembers()
            if len(members) > MAX_FILES:
                raise Rejected('too_many_files')
            seen, files, total = set(), {}, 0
            for member in members:
                name = member.name
                path = PurePosixPath(name)
                if (not name or path.is_absolute() or '\\' in name or
                    any(ord(c) < 32 for c in name) or '..' in path.parts or
                    name != path.as_posix() or name in seen or
                    not (name in (binaries | {'manifest.json', 'web'}) or name.startswith('web/')) or
                    not (member.isfile() or member.isdir()) or
                    (member.isdir() and not (name == 'web' or name.startswith('web/')))):
                    raise Rejected('unsafe_archive_entry')
                seen.add(name)
                total += member.size
                if (total > MAX_CONTENT or member.size > 32 * 1024 * 1024
                        or (name == 'manifest.json' and member.size > 1024 * 1024)):
                    raise Rejected('archive_content_limit')
                target = destination / name
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True, mode=0o755)
                    continue
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
                with tar.extractfile(member) as source, target.open('xb') as out:
                    shutil.copyfileobj(source, out)
                    out.flush()
                    os.fsync(out.fileno())
                target.chmod(0o755 if name in binaries else 0o644)
                files[name] = hashlib.sha256(target.read_bytes()).hexdigest()
            if not (binaries | {'manifest.json', 'web/index.html'}) <= files.keys():
                raise Rejected('incomplete_release')
            manifest = json.loads((destination / 'manifest.json').read_text())
            if manifest.get('identity') != pending['identity'] or manifest.get('files') != {k: v for k, v in files.items() if k != 'manifest.json'}:
                raise Rejected('manifest_mismatch')
            for directory in sorted((p for p in destination.rglob('*') if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
                directory.chmod(0o755)
                fsync_dir(directory)
            fsync_dir(destination)
            for binary in binaries:
                with (destination / binary).open('rb') as stream:
                    header = stream.read(20)
                if (len(header) != 20 or header[:6] != b'\x7fELF\x02\x01' or expected_machine is None
                        or struct.unpack('<H', header[18:20])[0] != expected_machine):
                    raise Rejected('wrong_binary_platform')

    def current(self):
        path = self.root / 'current'
        if not path.is_symlink():
            if path.exists():
                raise Rejected('current_is_not_symlink')
            return None
        value = os.readlink(path)
        if not re.fullmatch(r'releases/(?:[1-9][0-9]*-)?[0-9a-f]{40}', value) or not (self.root / value).is_dir():
            raise Rejected('invalid_current')
        return value

    def switch(self, value):
        link = self.root / 'current.next'
        link.unlink(missing_ok=True)
        link.symlink_to(value)
        os.replace(link, self.root / 'current')
        fsync_dir(self.root)

    def wait_health(self):
        for attempt in range(15):
            try:
                self.probe()
                return
            except (OSError, ValueError, Rejected):
                if attempt == 14:
                    raise Rejected('health_failed') from None
                time.sleep(1)

    def worker_target(self):
        current = self.worker_root / 'current'
        if not current.is_symlink():
            raise Rejected('invalid_worker_current')
        path = current.resolve(strict=True)
        if (path.parent != self.worker_root / 'releases' or not path.is_dir()
                or not re.fullmatch(r'(?:[1-9][0-9]*-)?[0-9a-f]{40}', path.name)):
            raise Rejected('invalid_worker_current')
        return path

    def switch_worker(self, value, name='current'):
        link = self.worker_root / (name + '.next')
        link.unlink(missing_ok=True)
        link.symlink_to(value)
        os.replace(link, self.worker_root / name)
        fsync_dir(self.worker_root)

    def wait_joint_health(self, expected):
        for attempt in range(40):
            try:
                self.worker.executable(expected, paused=False)
                self.worker.ready()
                return
            except (OSError, ValueError, http.client.HTTPException, Rejected, subprocess.SubprocessError):
                if attempt == 39:
                    raise Rejected('worker_readiness_failed') from None
                time.sleep(1)

    def apply_joint(self, state, pending):
        if self.worker is None:
            state['result'] = 'rollback_failed' if state.get('joint') else 'deferred'
            state['defer_reason'] = 'joint_release_policy_mismatch'
            self.save(state)
            return state
        token = pending['release']
        release = self.root / 'releases' / token
        worker_release = self.worker_root / 'releases' / token
        archive = self.state_dir / pending['archive']
        recovering = state['result'] == 'applying'
        journal = state.get('joint') if recovering else None
        phase = journal['phase'] if journal else None
        try:
            if recovering:
                if phase == 'committed' and pending['identity'] == state['candidate']:
                    current = self.worker.wait_executable(worker_release / 'itmap-worker')
                    self.service.verify_executable(release / 'itmap')
                    self.wait_health()
                    if current['paused']:
                        self.worker.resume(token, current)
                    elif self.worker.hold_token() is not None:
                        raise Rejected('worker_hold_conflict')
                    self.wait_joint_health(worker_release / 'itmap-worker')
                    return self.finish_joint(state, pending)
                raise Rejected('interrupted_joint_release')
            if pending['identity'] != state['candidate']:
                state.update(result='superseded', pending=None)
                self.save(state)
                return state
            if hashlib.sha256(archive.read_bytes()).hexdigest() != pending['digest']:
                raise Rejected('archive_digest_mismatch')
            temp = Path(tempfile.mkdtemp(prefix='.joint-', dir=release.parent))
            worker_temp = None
            try:
                self.unpack(archive, temp, pending)
                worker_temp = Path(tempfile.mkdtemp(prefix='.joint-', dir=worker_release.parent))
                shutil.copy2(temp / 'itmap-worker', worker_temp / 'itmap-worker')
                with (worker_temp / 'itmap-worker').open('rb') as stream:
                    os.fsync(stream.fileno())
                worker_temp.chmod(0o755)
                fsync_dir(worker_temp)
                temp.chmod(0o755)
                self.install_joint_tree(temp, release)
                self.install_joint_tree(worker_temp, worker_release)
            finally:
                if temp.exists():
                    shutil.rmtree(temp)
                if worker_temp is not None and worker_temp.exists():
                    shutil.rmtree(worker_temp)
            old_api = self.current()
            old_worker = self.worker_target()
            if not old_api:
                raise Rejected('joint_previous_api_unavailable')
            self.service.verify_executable(self.root / old_api / 'itmap')
            old_instance = self.worker.executable(old_worker / 'itmap-worker', paused=False)
            journal = {'phase': 'draining', 'api': old_api, 'worker': str(old_worker), 'instance': old_instance}
            state.update(result='applying', joint=journal)
            self.save(state)
            phase = 'draining'
            instance = self.worker.drain(token)
            if instance['instance'] != old_instance['instance'] or instance['pid'] != old_instance['pid']:
                raise Rejected('worker_instance_changed')
            journal['phase'] = phase = 'switching'
            self.save(state)
            self.worker.service('stop')
            self.switch_worker(worker_release)
            self.switch('releases/' + token)
            self.service.restart()
            self.wait_health()
            self.service.verify_executable(release / 'itmap')
            self.worker.service('start')
            current = self.worker.wait_executable(worker_release / 'itmap-worker', paused=True)
            self.switch_worker(old_worker, 'previous')
            journal['phase'] = phase = 'committed'
            self.save(state)
            self.worker.resume(token, current)
            self.wait_joint_health(worker_release / 'itmap-worker')
            return self.finish_joint(state, pending)
        except Exception as error:
            reason = str(error) if isinstance(error, Rejected) else ''
            state['defer_reason'] = reason if re.fullmatch(r'[a-z_]{1,80}', reason) else 'joint_release_check_failed'
            try:
                if phase == 'draining':
                    if self.worker.hold_token() == token:
                        current = self.worker.instance()
                        if current['paused']:
                            self.worker.resume(token, journal['instance'])
                        elif current['instance'] == journal['instance']['instance'] and current['pid'] == journal['instance']['pid']:
                            self.worker.hold.unlink()
                            fsync_dir(self.worker.hold.parent)
                        else:
                            raise Rejected('worker_instance_changed')
                    state['result'] = 'deferred'
                elif phase in ('switching', 'committed'):
                    if phase == 'committed':
                        if self.worker.active():
                            self.worker.drain(token)
                        else:
                            self.worker.create_hold(token)
                        journal['phase'] = 'switching'
                        self.save(state)
                    self.rollback_joint(token, journal)
                    state['result'] = 'failed_rolled_back'
                else:
                    state['result'] = 'deferred'
            except Exception:
                state['result'] = 'rollback_failed'
            self.save(state)
            return state

    def install_joint_tree(self, staging, destination):
        if not destination.exists():
            os.replace(staging, destination)
            fsync_dir(destination.parent)
            return
        expected = {path.relative_to(staging) for path in staging.rglob('*') if path.is_file()}
        existing = {path.relative_to(destination) for path in destination.rglob('*') if path.is_file()}
        if destination.is_symlink() or expected != existing or any(
                (destination / name).is_symlink()
                or hashlib.sha256((staging / name).read_bytes()).digest()
                != hashlib.sha256((destination / name).read_bytes()).digest()
                for name in expected):
            raise Rejected('immutable_release_conflict')

    def rollback_joint(self, token, journal):
        if self.worker.hold_token() != token:
            raise Rejected('worker_hold_conflict')
        if self.worker.active():
            current = self.worker_target()
            self.worker.wait_executable(current / 'itmap-worker', paused=True)
            self.worker.service('stop')
        old_worker = Path(journal['worker'])
        self.switch_worker(old_worker)
        self.switch(journal['api'])
        self.service.restart()
        self.wait_health()
        self.service.verify_executable(self.root / journal['api'] / 'itmap')
        self.worker.service('start')
        current = self.worker.wait_executable(old_worker / 'itmap-worker', paused=True)
        self.worker.resume(token, current)
        self.wait_joint_health(old_worker / 'itmap-worker')

    def finish_joint(self, state, pending):
        state.update(result='success', joint=None, current={'identity': pending['identity'],
                     'digest': pending['digest'], 'release': pending['release']})
        self.save(state)
        return state

    def apply(self):
        # Worker is detached by systemd, not a child tied to the SSH connection.
        with self.locked():
            state = self.load()
            pending = state.get('pending')
            if not pending or state['result'] not in ('queued', 'applying'):
                return state
            if pending.get('joint'):
                return self.apply_joint(state, pending)
            if self.worker is not None:
                state['result'] = 'deferred'
                self.save(state)
                return state
            if pending['identity'] != state['candidate']:
                if state['result'] == 'applying':
                    try:
                        if state.get('previous'):
                            self.switch(state['previous'])
                            self.service.restart()
                            self.wait_health()
                        else:
                            self.service.stop()
                            (self.root / 'current').unlink(missing_ok=True)
                    except Exception:
                        state['result'] = 'rollback_failed'
                        self.save(state)
                        return state
                state.update(result='superseded', pending=None)
                self.save(state)
                return state
            release = self.root / 'releases' / pending['release']
            archive = self.state_dir / pending['archive']
            recovering = state['result'] == 'applying'
            old = state.get('previous') if recovering else self.current()
            # Persist the rollback point BEFORE changing the symlink; retry after
            # a crash never mistakes an interrupted new release for previous.
            state.update(result='applying', previous=old)
            self.save(state)
            switched = False
            try:
                if hashlib.sha256(archive.read_bytes()).hexdigest() != pending['digest']:
                    raise Rejected('archive_digest_mismatch')
                if not release.exists():
                    temp = Path(tempfile.mkdtemp(prefix='.release-', dir=self.root / 'releases'))
                    try:
                        self.unpack(archive, temp, pending)
                        temp.chmod(0o755)
                        os.replace(temp, release)
                        fsync_dir(release.parent)
                    finally:
                        if temp.exists():
                            shutil.rmtree(temp)
                switched = True
                self.switch('releases/' + release.name)
                self.service.restart()
                self.wait_health()
                state.update(result='success', current={'identity': pending['identity'],
                             'digest': pending['digest'], 'release': release.name})
            except Exception:
                # Never print subprocess output, raw input or arbitrary errors.
                try:
                    if old and (switched or recovering):
                        self.switch(old)
                        self.service.restart()
                        self.wait_health()
                    elif not old and (switched or recovering):
                        self.service.stop()
                        (self.root / 'current').unlink(missing_ok=True)
                        fsync_dir(self.root)
                    state['result'] = 'failed_rolled_back'
                except Exception:
                    state['result'] = 'rollback_failed'
                self.save(state)
                return state
            self.save(state)
            return state

    def status(self):
        # Atomic JSON can be read while activation holds the lock. A short
        # status request must not wait for a full restart/rollback timeout.
        with (self.state_dir / 'lock').open('a') as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                return self.load()
            state = self.load()
            if state['result'] in ('queued', 'applying') and state.get('pending'):
                self.service.launch(state['pending']['identity']['sequence'])
            return state


def trusted_directory(path):
    # CLI paths are fixed, installed by root. Never traverse writable/symlink
    # parents on behalf of a deploy key (tests use Receiver directly).
    for directory in (path, *path.parents):
        info = directory.lstat()
        if directory.is_symlink() or not directory.is_dir() or info.st_uid != 0 or info.st_mode & 0o022:
            raise Rejected('unsafe_installation_permissions')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--slot', choices=('main', 'dev'), required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise Rejected('root_receiver_required')
    os.umask(0o077)
    trusted_directory(Path(f'/opt/itmap/{args.slot}'))
    trusted_directory(Path(f'/var/lib/itmap-deploy/{args.slot}'))
    worker_root = Path('/opt/itmap/worker')
    state_root = Path(f'/var/lib/itmap-deploy/{args.slot}')
    policy = state_root / 'joint-release-enabled'
    worker = None
    if args.slot == 'main' and os.path.lexists(policy):
        info = policy.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise Rejected('unsafe_joint_policy')
        trusted_directory(worker_root / 'releases')
        worker = WorkerDrain(worker_root)
    receiver = Receiver(args.slot, worker_root=worker_root, worker=worker)
    if args.apply:
        result = receiver.apply()
    else:
        signal.alarm(150)  # Includes slow/partial SSH upload, never the worker.
        header = sys.stdin.buffer.readline(8193)
        if len(header) > 8192 or not header.endswith(b'\n'):
            raise Rejected('invalid_header')
        request = json.loads(header)
        if not isinstance(request, dict):
            raise Rejected('invalid_header')
        operation = request.get('operation')
        if operation == 'register':
            result = receiver.register(request)
        elif operation == 'invalidate':
            result = receiver.invalidate(request)
        elif operation == 'upload':
            result = receiver.upload(request, sys.stdin.buffer)
        elif operation == 'status':
            result = receiver.status()
        else:
            raise Rejected('invalid_operation')
    print(json.dumps(result, ensure_ascii=True, sort_keys=True), flush=True)
    return 1 if result['result'] in ('failed_rolled_back', 'rollback_failed') else 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Rejected as error:
        print(json.dumps({'error': str(error)}), flush=True)
        sys.exit(1)
    except Exception:
        print('{"error":"receiver_failed"}', flush=True)
        sys.exit(1)
