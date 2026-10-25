import hashlib
import importlib.util
import io
import json
import multiprocessing
import os
from pathlib import Path
import struct
import sys
import tarfile
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('receiver', Path(__file__).resolve().parents[1] / 'receive_release.py')
r = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(r)


def identity(sequence=1, branch='codex/test', sha=None):
    return dict(repository=r.REPOSITORY, workflow=r.WORKFLOW, ref='refs/heads/' + branch,
                sha=sha or (f'{sequence:040x}'), sequence=sequence, run_id=100 + sequence)


def archive(request, extra=None, wrong_manifest=False):
    elf = bytearray(64)
    elf[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<H', elf, 18, {'x86_64': 62, 'aarch64': 183}[r.platform.machine()])
    files = {'itmap': bytes(elf), 'web/index.html': b'<div id="root"></div>',
             'web/assets/app.js': b'// CI SYNTHETIC RELEASE'}
    manifest = {'identity': request, 'files': {k: hashlib.sha256(v).hexdigest() for k, v in files.items()}}
    if wrong_manifest:
        manifest['identity'] = identity(99)
    files['manifest.json'] = json.dumps(manifest).encode()
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w') as tar:
        for name, data in files.items():
            member = tarfile.TarInfo(name)
            member.size = len(data)
            tar.addfile(member, io.BytesIO(data))
        if extra:
            tar.addfile(extra, io.BytesIO(b'x' * extra.size))
    data = output.getvalue()
    return {**request, 'size': len(data), 'digest': hashlib.sha256(data).hexdigest()}, data


class Service:
    def __init__(self):
        self.calls = []

    def launch(self, sequence):
        self.calls.append(('launch', sequence))

    def restart(self):
        self.calls.append(('restart',))

    def stop(self):
        self.calls.append(('stop',))


class ReceiverTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.service = Service()
        self.receiver = r.Receiver('dev', self.base / 'dev', self.base / 'state-dev',
                                   self.service, lambda: None)

    def publish(self, request):
        self.receiver.register(request)
        header, data = archive(request)
        self.receiver.upload(header, io.BytesIO(data))
        return self.receiver.apply()

    def test_success_manifest_permissions_and_status(self):
        state = self.publish(identity())
        self.assertEqual(state['result'], 'success')
        self.assertEqual(self.receiver.current(), 'releases/1-' + identity()['sha'])
        release = self.receiver.root / self.receiver.current()
        self.assertEqual((release / 'itmap').stat().st_mode & 0o777, 0o755)
        self.assertEqual((release / 'web/assets').stat().st_mode & 0o777, 0o755)
        self.assertEqual((release / 'web/index.html').stat().st_mode & 0o777, 0o644)
        self.assertEqual(self.receiver.status()['current']['identity'], identity())

    def test_late_older_push_and_rerun_cannot_replace_latest(self):
        self.publish(identity(2))
        for candidate in [identity(1), identity(2, 'codex/different')]:
            with self.assertRaises(r.Rejected):
                self.receiver.register(candidate)
        self.assertEqual(self.receiver.register(identity(2))['result'], 'success')
        old_header, data = archive(identity(1))
        with self.assertRaisesRegex(r.Rejected, 'superseded'):
            self.receiver.upload(old_header, io.BytesIO(data))
        self.assertEqual(self.receiver.current(), 'releases/2-' + identity(2)['sha'])

    def test_red_candidate_keeps_previous_and_blocks_old_upload(self):
        self.publish(identity(1))
        self.receiver.register(identity(2))  # Simulated failed CI: no upload.
        header, data = archive(identity(1))
        with self.assertRaises(r.Rejected):
            self.receiver.upload(header, io.BytesIO(data))
        self.assertTrue(self.receiver.current().startswith('releases/1-'))

    def test_queued_superseded_before_activation(self):
        self.receiver.register(identity(1))
        header, data = archive(identity(1))
        self.receiver.upload(header, io.BytesIO(data))
        self.receiver.register(identity(2))
        self.assertEqual(self.receiver.apply()['result'], 'superseded')
        self.assertIsNone(self.receiver.current())

    def test_failed_release_rolls_back_but_reports_failure(self):
        self.publish(identity(1))
        self.receiver.probe = lambda: (_ for _ in ()).throw(r.Rejected('bad')) if self.receiver.current().startswith('releases/2-') else None
        with patch.object(r.time, 'sleep'):
            state = self.publish(identity(2))
        self.assertEqual(state['result'], 'failed_rolled_back')
        self.assertTrue(self.receiver.current().startswith('releases/1-'))
        self.assertEqual(state['current']['identity'], identity(1))

    def test_rollback_failure_is_distinct(self):
        self.publish(identity(1))
        self.receiver.probe = lambda: (_ for _ in ()).throw(r.Rejected('bad'))
        with patch.object(r.time, 'sleep'):
            self.assertEqual(self.publish(identity(2))['result'], 'rollback_failed')

    def test_first_release_failure_stops_process(self):
        self.receiver.probe = lambda: (_ for _ in ()).throw(r.Rejected('bad'))
        with patch.object(r.time, 'sleep'):
            state = self.publish(identity())
        self.assertEqual(state['result'], 'failed_rolled_back')
        self.assertIsNone(self.receiver.current())
        self.assertIn(('stop',), self.service.calls)

    def test_crash_after_switch_recovers_original_previous(self):
        self.publish(identity(1))
        second = identity(2)
        self.receiver.register(second)
        header, data = archive(second)
        self.receiver.upload(header, io.BytesIO(data))
        state = self.receiver.load()
        state.update(result='applying', previous=self.receiver.current())
        self.receiver.save(state)
        target = self.receiver.root / 'releases' / ('2-' + second['sha'])
        target.mkdir()
        self.receiver.switch('releases/' + target.name)
        self.receiver.register(identity(3))
        self.assertEqual(self.receiver.apply()['result'], 'superseded')
        self.assertTrue(self.receiver.current().startswith('releases/1-'))

    def test_queue_start_gap_survives_receiver_restart(self):
        self.receiver.register(identity())
        header, data = archive(identity())
        self.receiver.upload(header, io.BytesIO(data))
        restarted = r.Receiver('dev', self.receiver.root, self.receiver.state_dir, self.service, lambda: None)
        self.assertEqual(restarted.status()['result'], 'queued')
        self.assertGreaterEqual(self.service.calls.count(('launch', 1)), 2)
        self.assertEqual(restarted.apply()['result'], 'success')

    def test_deletion_does_not_resurrect_or_cancel_other_branch(self):
        self.publish(identity(1))
        self.receiver.invalidate(identity(3, 'codex/other'))
        self.assertEqual(self.receiver.load()['candidate'], identity(1))
        with self.assertRaises(r.Rejected):
            self.receiver.register(identity(2, 'codex/other'))
        self.receiver.invalidate(identity(4))
        self.assertIsNone(self.receiver.load()['candidate'])
        with self.assertRaises(r.Rejected):
            self.receiver.register(identity(1))
        self.assertTrue(self.receiver.current().startswith('releases/1-'))

    def test_scope_identity_and_epoch_are_fail_closed(self):
        for candidate in [identity(branch='main'), {**identity(), 'repository': 'wrong/repo'},
                          {**identity(), 'workflow': 'publish-v2'}, {**identity(), 'sequence': True},
                          {**identity(), 'sha': '../bad'}, identity(branch='bad/../ref')]:
            with self.assertRaises(r.Rejected):
                self.receiver.register(candidate)
        main = r.Receiver('main', self.base / 'main', self.base / 'state-main', Service(), lambda: None)
        main.register(identity(branch='main'))
        self.assertIsNone(self.receiver.load()['candidate'])

    def test_archive_digest_length_manifest_and_traversal(self):
        self.receiver.register(identity())
        header, data = archive(identity())
        for bad_header, bad_data in [({**header, 'digest': '0'*64}, data),
                                     (header, data[:-1]), (header, data+b'x')]:
            with self.assertRaises(r.Rejected):
                self.receiver.upload(bad_header, io.BytesIO(bad_data))
        for number, (name, kind) in enumerate([('../outside', tarfile.REGTYPE), ('/outside', tarfile.REGTYPE),
                           ('web/link', tarfile.SYMTYPE), ('web/hard', tarfile.LNKTYPE),
                           ('web/fifo', tarfile.FIFOTYPE), ('web/../escape', tarfile.REGTYPE),
                           ('itmap', tarfile.REGTYPE), ('private.env', tarfile.REGTYPE)], start=2):
            with self.subTest(name=name, kind=kind):
                extra = tarfile.TarInfo(name)
                extra.type = kind
                extra.linkname = '/etc/itmap/main.env'
                extra.size = 1 if kind == tarfile.REGTYPE else 0
                request = identity(number)
                self.receiver.register(request)
                h, payload = archive(request, extra=extra)
                self.receiver.upload(h, io.BytesIO(payload))
                self.assertEqual(self.receiver.apply()['result'], 'failed_rolled_back')
                self.assertFalse((self.base / 'outside').exists())
        self.receiver.register(identity(20))
        h, payload = archive(identity(20), wrong_manifest=True)
        self.receiver.upload(h, io.BytesIO(payload))
        self.assertEqual(self.receiver.apply()['result'], 'failed_rolled_back')

    def test_independent_processes_keep_newest_registration(self):
        ctx = multiprocessing.get_context('fork')
        gate = ctx.Barrier(3)
        def register(sequence):
            receiver = r.Receiver('dev', self.receiver.root, self.receiver.state_dir, Service(), lambda: None)
            gate.wait(timeout=5)
            try:
                receiver.register(identity(sequence))
            except r.Rejected as error:
                if str(error) != 'superseded':
                    raise
        workers = [ctx.Process(target=register, args=(seq,)) for seq in (1, 2)]
        for worker in workers:
            worker.start()
        gate.wait(timeout=5)
        for worker in workers:
            worker.join(timeout=5)
            self.assertEqual(worker.exitcode, 0)
        self.assertEqual(self.receiver.load()['candidate'], identity(2))

    def test_successful_upload_replay_keeps_original_rollback_point(self):
        request = identity()
        self.publish(request)
        before = self.receiver.load()
        header, data = archive(request)
        self.assertEqual(self.receiver.upload(header, io.BytesIO(data))['result'], 'success')
        self.assertEqual(self.receiver.load(), before)
        self.assertEqual(self.service.calls.count(('restart',)), 1)

    def test_corrupt_state_is_not_reset(self):
        self.receiver.state_file.write_text('{')
        with self.assertRaisesRegex(r.Rejected, 'state_unreadable'):
            self.receiver.register(identity())
        self.assertEqual(self.receiver.state_file.read_text(), '{')

    def test_actual_health_rejects_db_failure_and_redirect(self):
        class Handler(BaseHTTPRequestHandler):
            mode = 'good'
            def do_GET(self):
                status, body = (200, {'status': 'alive'}) if self.path.endswith('live') else (503, {'status': 'not_ready', 'reason': 'business_api_not_migrated'})
                if self.mode == 'db' and self.path.endswith('ready'):
                    body['reason'] = 'mariadb_unavailable'
                if self.mode == 'redirect':
                    status = 302
                    self.send_response(status)
                    self.send_header('Location', 'https://example.invalid/')
                else:
                    self.send_response(status)
                self.end_headers()
                self.wfile.write(json.dumps(body).encode())
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f'http://127.0.0.1:{server.server_port}'
            r.health(url)
            for mode in ('db', 'redirect'):
                Handler.mode = mode
                with self.assertRaises(r.Rejected):
                    r.health(url)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == '__main__':
    unittest.main()
