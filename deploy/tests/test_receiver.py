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


def archive(request, extra=None, wrong_manifest=False, joint=False):
    elf = bytearray(64)
    elf[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<H', elf, 18, {'x86_64': 62, 'aarch64': 183}[r.platform.machine()])
    files = {'itmap': bytes(elf), 'web/index.html': b'<div id="root"></div>',
             'web/assets/app.js': b'// CI SYNTHETIC RELEASE'}
    if joint:
        files['itmap-worker'] = bytes(elf)
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


class SystemdInterpreterTests(unittest.TestCase):
    def test_worker_keeps_selected_python_on_focal(self):
        # Entry may use an isolated supported Python while /usr/bin/python3 is
        # Focal's 3.8. The detached worker must keep the selected interpreter.
        selected = '/opt/itmap-python/bin/python3.12'
        with patch.object(r.sys, 'executable', selected), patch.object(r.subprocess, 'run') as run:
            run.return_value.returncode = 0
            r.Systemd('dev').launch(7)
        command = run.call_args.args[0]
        self.assertEqual(command[command.index(selected):],
                         [selected, '-I', '/usr/local/libexec/itmap-receive.py', '--slot', 'dev', '--apply'])
        self.assertIn('itmap-release-dev-7.service', command)
        self.assertNotIn('/usr/bin/python3', command)


class WorkerDrainTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.worker = r.WorkerDrain(Path(temp.name))
        self.instance = {'instance': 'a' * 32, 'pid': 42, 'paused': True}
        self.baseline = {'count': 2, 'digest': 'a' * 64}

    def test_hold_is_idempotent_but_different_release_cannot_replace_it(self):
        self.worker.create_hold('release-1')
        self.worker.create_hold('release-1')
        self.assertEqual(self.worker.hold_token(), 'release-1')
        self.assertEqual(self.worker.hold.stat().st_mode & 0o777, 0o644)
        with self.assertRaisesRegex(r.Rejected, 'worker_hold_conflict'):
            self.worker.create_hold('release-2')
        self.assertEqual(self.worker.hold_token(), 'release-1')

    def test_drain_and_resume_same_instance(self):
        resumed = dict(self.instance, paused=False)
        with (patch.object(self.worker, 'instance', side_effect=[self.instance, self.instance, self.instance, resumed]),
              patch.object(self.worker, 'message', side_effect=[{'state': 'draining'}, {'state': 'held'}, {'state': 'resumed'}]) as message,
              patch.object(self.worker, 'verify_idle') as idle,
              patch.object(self.worker, 'ledger', return_value=self.baseline), patch.object(r.time, 'sleep')):
            actual = self.worker.drain('release-1')
            idle.assert_called_once_with(self.baseline)
            self.assertEqual(actual, self.instance)
            request = {'instance': self.instance['instance'], 'token': 'release-1'}
            message.assert_called_with('/pause', request)
            self.worker.resume('release-1', actual)
            message.assert_called_with('/resume', request)
        self.assertFalse(self.worker.hold.exists())

    def test_drain_defers_on_unsettled_ledger_or_process_change(self):
        for changed in (False, True):
            with self.subTest(changed=changed):
                other = dict(self.instance, pid=43) if changed else self.instance
                problem = None if changed else r.Rejected('worker_unsettled_operations')
                with (patch.object(self.worker, 'instance', side_effect=[self.instance, other]),
                      patch.object(self.worker, 'message', return_value={'state': 'held'}),
                      patch.object(self.worker, 'ledger', return_value=self.baseline),
                      patch.object(self.worker, 'verify_idle', side_effect=problem)):
                    with self.assertRaisesRegex(r.Rejected, 'worker_instance_changed' if changed else 'worker_unsettled_operations'):
                        self.worker.drain('release-1')
                self.assertEqual(self.worker.hold_token(), 'release-1')

    def test_unsettled_diagnostics_refuse_release(self):
        queue = {'scan': {'running': 0}, 'pipeline': {'running': 0}}
        recovery = {key: 0 for key in (
            'actorCurrentUnresolved', 'currentAIReservedRunning', 'currentAIReservedOther',
            'currentAIZeroReserveProviderUnknown', 'actorUnknown', 'actorReconcile',
            'schedulerHistoricalUnknown', 'schedulerOrphanUnknown',
            'schedulerHistoricalZeroReserveProviderUnknown', 'schedulerOrphanZeroReserveProviderUnknown')}
        recovery['currentAIReservedOther'] = 2
        for unsettled, evidence, expected in (
                (0, self.baseline, None),
                (1, self.baseline, 'worker_unsettled_operations'),
                (False, self.baseline, 'worker_unsettled_operations'),
                (0, dict(self.baseline, digest='b' * 64), 'worker_ledger_changed')):
            with (self.subTest(unsettled=unsettled, expected=expected),
                  patch.object(r.http.client, 'HTTPConnection') as connection,
                  patch.object(self.worker, 'ledger', return_value=evidence)):
                recovery['actorCurrentUnresolved'] = unsettled
                response = connection.return_value.getresponse.return_value.__enter__.return_value
                response.status = 200
                response.read.side_effect = [json.dumps(snapshot).encode() for snapshot in (queue, recovery)]
                if expected is None:
                    self.worker.verify_idle(self.baseline)
                else:
                    with self.assertRaisesRegex(r.Rejected, expected):
                        self.worker.verify_idle(self.baseline)


class JointReceiverTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        self.worker_root = root / 'worker'
        self.api_root = root / 'api'
        self.worker = r.WorkerDrain(self.worker_root)
        self.service = Service()
        self.service.verify_executable = lambda expected: self.assertTrue(expected.is_file())
        self.receiver = r.Receiver('main', self.api_root, root / 'state', self.service, lambda: None,
                                   worker_root=self.worker_root, worker=self.worker)
        self.old_api = self.api_root / 'releases' / ('1' * 40)
        self.old_worker = self.worker_root / 'releases' / ('1' * 40)
        for release, binary in ((self.old_api, 'itmap'), (self.old_worker, 'itmap-worker')):
            release.mkdir(parents=True)
            (release / binary).write_bytes(b'previous')
            (release.parents[1] / 'current').symlink_to(release)
        # Existing API receiver uses a relative pointer.
        (self.api_root / 'current').unlink()
        (self.api_root / 'current').symlink_to('releases/' + self.old_api.name)
        self.request = identity(2, 'main')
        self.header, self.body = archive(self.request, joint=True)
        self.header['joint'] = True
        self.receiver.register(self.request)
        self.receiver.upload(self.header, io.BytesIO(self.body))
        self.current = {'instance': 'a' * 32, 'pid': 42, 'paused': False}
        self.paused = dict(self.current, paused=True)

    def test_joint_release_success_and_repeat(self):
        with (patch.object(self.worker, 'executable', return_value=self.current),
              patch.object(self.worker, 'drain', return_value=self.paused),
              patch.object(self.worker, 'service') as service,
              patch.object(self.worker, 'wait_executable', return_value=self.paused),
              patch.object(self.worker, 'resume') as resume,
              patch.object(self.worker, 'ready')):
            state = self.receiver.apply()
            self.assertEqual(state['result'], 'success')
            self.assertEqual((self.worker_root / 'current').resolve().name, state['current']['release'])
            self.assertEqual((self.worker_root / 'previous').resolve(), self.old_worker)
            self.assertEqual([call.args for call in service.call_args_list], [('stop',), ('start',)])
            resume.assert_called_once()
            self.assertEqual(self.receiver.upload(self.header, io.BytesIO(self.body))['result'], 'success')
            self.assertEqual(self.receiver.apply()['result'], 'success')
            self.assertEqual(service.call_count, 2)

    def test_active_paid_operation_defers_without_switching(self):
        with (patch.object(self.worker, 'executable', return_value=self.current),
              patch.object(self.worker, 'drain', side_effect=r.Rejected('worker_unsettled_operations')),
              patch.object(self.worker, 'service') as service):
            self.assertEqual(self.receiver.apply()['result'], 'deferred')
            service.assert_not_called()
        self.assertEqual((self.worker_root / 'current').resolve(), self.old_worker)
        self.assertEqual((self.api_root / 'current').resolve(), self.old_api)

    def test_failure_restores_previous_both(self):
        def drain(token):
            self.worker.create_hold(token)
            return self.paused
        with (patch.object(self.worker, 'executable', return_value=self.current),
              patch.object(self.worker, 'drain', side_effect=drain),
              patch.object(self.worker, 'active', return_value=False),
              patch.object(self.worker, 'service'),
              patch.object(self.worker, 'ready'),
              patch.object(self.worker, 'resume'),
              patch.object(self.worker, 'wait_executable', side_effect=[r.Rejected('failed_start'), self.paused])):
            self.assertEqual(self.receiver.apply()['result'], 'failed_rolled_back')
        self.assertEqual((self.worker_root / 'current').resolve(), self.old_worker)
        self.assertEqual((self.api_root / 'current').resolve(), self.old_api)

    def test_interrupted_switch_restores_both_targets(self):
        token = self.receiver.load()['pending']['release']
        new_worker = self.worker_root / 'releases' / token
        new_worker.mkdir()
        self.receiver.switch_worker(new_worker)
        self.worker.create_hold(token)
        state = self.receiver.load()
        state.update(result='applying', joint={
            'phase': 'switching', 'api': 'releases/' + self.old_api.name,
            'worker': str(self.old_worker), 'instance': self.current,
        })
        self.receiver.save(state)
        with (patch.object(self.worker, 'active', return_value=False),
              patch.object(self.worker, 'service'),
              patch.object(self.worker, 'ready'),
              patch.object(self.worker, 'resume'),
              patch.object(self.worker, 'executable', return_value=self.current),
              patch.object(self.worker, 'wait_executable', return_value=self.paused)):
            self.assertEqual(self.receiver.apply()['result'], 'failed_rolled_back')
        self.assertEqual((self.worker_root / 'current').resolve(), self.old_worker)
        self.assertEqual((self.api_root / 'current').resolve(), self.old_api)

    def test_failed_rollback_blocks_following_release(self):
        state = self.receiver.load()
        state.update(result='applying', joint={
            'phase': 'switching', 'api': 'releases/' + self.old_api.name,
            'worker': str(self.old_worker), 'instance': self.current,
        })
        self.receiver.save(state)
        self.assertEqual(self.receiver.apply()['result'], 'rollback_failed')
        with self.assertRaisesRegex(r.Rejected, 'joint_rollback_requires_operator'):
            self.receiver.register(identity(3, 'main'))

    def test_joint_requires_local_opt_in(self):
        self.receiver.worker = None
        with self.assertRaisesRegex(r.Rejected, 'joint_release_policy_mismatch'):
            self.receiver.upload(self.header, io.BytesIO(self.body))


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
        self.assertFalse(any(call[0] in ('restart', 'stop') for call in self.service.calls))

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

    def test_existing_manual_sha_release_is_a_valid_rollback_point(self):
        self.publish(identity(1))
        previous = self.receiver.root / self.receiver.current()
        manual = self.receiver.root / 'releases' / identity(1)['sha']
        previous.rename(manual)
        self.receiver.switch('releases/' + manual.name)
        self.receiver.probe = lambda: (_ for _ in ()).throw(r.Rejected('bad')) if self.receiver.current().startswith('releases/2-') else None
        with patch.object(r.time, 'sleep'):
            self.assertEqual(self.publish(identity(2))['result'], 'failed_rolled_back')
        self.assertEqual(self.receiver.current(), 'releases/' + manual.name)

    def test_corrupt_state_is_not_reset(self):
        self.receiver.state_file.write_text('{')
        with self.assertRaisesRegex(r.Rejected, 'state_unreadable'):
            self.receiver.register(identity())
        self.assertEqual(self.receiver.state_file.read_text(), '{')

    def test_actual_health_rejects_db_failure_and_redirect(self):
        class Handler(BaseHTTPRequestHandler):
            mode = 'good'
            def do_GET(self):
                if self.path == '/':
                    self.send_response(503 if self.mode == 'spa' else 200)
                    self.send_header('Content-Type', 'text/html')
                    self.end_headers()
                    self.wfile.write(b'<div id="root"></div>')
                    return
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
            for mode in ('db', 'redirect', 'spa'):
                Handler.mode = mode
                with self.assertRaises(r.Rejected):
                    r.health(url)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == '__main__':
    unittest.main()
