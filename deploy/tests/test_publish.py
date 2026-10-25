import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import publish_release as p
import receive_release as r
from test_receiver import Service, archive, identity


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def env(self):
        return dict(GITHUB_REPOSITORY=r.REPOSITORY, GITHUB_REF='refs/heads/codex/test',
                    GITHUB_SHA='1'*40, GITHUB_RUN_NUMBER='10', GITHUB_RUN_ID='20')

    def test_push_delete_dispatch_and_rerun_selection(self):
        env = self.env()
        for event in ('push', 'workflow_dispatch'):
            slot, op, request = p.event_request(event, {'ref': env['GITHUB_REF']}, env)
            self.assertEqual((slot, op), ('dev', 'register'))
            self.assertEqual(request['sequence'], 10)
        slot, op, request = p.event_request('delete', {'ref':'codex/deleted','ref_type':'branch'}, {**env, 'GITHUB_REF':'refs/heads/main'})
        self.assertEqual((slot, op, request['ref']), ('dev', 'invalidate', 'refs/heads/codex/deleted'))
        self.assertEqual(p.event_request('push', {'ref':'refs/heads/main'}, {**env, 'GITHUB_REF':'refs/heads/main'})[0], 'main')
        first = p.event_request('push', {'ref':env['GITHUB_REF']}, {**env, 'GITHUB_RUN_ATTEMPT':'1'})
        self.assertEqual(first, p.event_request('push', {'ref':env['GITHUB_REF']}, {**env, 'GITHUB_RUN_ATTEMPT':'2'}))
        for event, payload, values in [('pull_request',{},env), ('delete',{'ref':'v1','ref_type':'tag'},env),
                                       ('push',{'ref':'refs/heads/wrong'},env),
                                       ('push',{'ref':env['GITHUB_REF']},{**env,'GITHUB_REPOSITORY':'other/repo'})]:
            with self.assertRaises(p.Rejected):
                p.event_request(event, payload, values)

    def inputs(self):
        header, data = archive(identity())
        import tarfile
        web = self.root / 'web'
        web.mkdir(exist_ok=True)
        with tarfile.open(fileobj=io.BytesIO(data)) as tar:
            binary = self.root / 'itmap'
            binary.write_bytes(tar.extractfile('itmap').read())
            (web / 'index.html').write_bytes(b'<div id="root"></div>')
        return binary, web

    def test_package_is_deterministic_and_accepted_by_receiver(self):
        binary, web = self.inputs()
        first, second = self.root / 'first.tar', self.root / 'second.tar'
        a = p.package(identity(), binary, web, first, verify_checkout=False)
        b = p.package(identity(), binary, web, second, verify_checkout=False)
        self.assertEqual(a, b)
        receiver = r.Receiver('dev', self.root / 'dev', self.root / 'state', Service(), lambda: None)
        receiver.register(identity())
        receiver.upload({**identity(), **a}, io.BytesIO(first.read_bytes()))
        self.assertEqual(receiver.apply()['result'], 'success')

    def test_package_refuses_symlink_missing_html_and_wrong_checkout(self):
        binary, web = self.inputs()
        (web / 'link').symlink_to('/etc/passwd')
        with self.assertRaises(p.Rejected):
            p.package(identity(), binary, web, self.root / 'bad.tar', verify_checkout=False)
        (web / 'link').unlink()
        (web / 'index.html').unlink()
        with self.assertRaises(p.Rejected):
            p.package(identity(), binary, web, self.root / 'bad.tar', verify_checkout=False)
        with self.assertRaisesRegex(p.Rejected, 'checkout_sha_or_content_mismatch'):
            p.package(identity(), binary, web, self.root / 'bad.tar')

    def client(self):
        client = p.Client({'DEPLOY_SSH_KEY':'CI_SYNTHETIC_KEY',
                           'DEPLOY_KNOWN_HOSTS':'ci.example.invalid ssh-ed25519 CI_SYNTHETIC_HOST_KEY',
                           'DEPLOY_HOST':'ci.example.invalid','DEPLOY_USER':'itmap-deploy-dev',
                           'GH_TOKEN':'CI_SYNTHETIC_READ_TOKEN'})
        self.addCleanup(client.close)
        return client

    def test_ssh_is_strict_and_key_is_not_passed_as_argument_or_environment(self):
        client = self.client()
        joined = ' '.join(client.command)
        self.assertIn('StrictHostKeyChecking=yes', joined)
        self.assertIn('IdentitiesOnly=yes', joined)
        self.assertNotIn('CI_SYNTHETIC_KEY', joined)
        self.assertNotIn('DEPLOY_SSH_KEY', client.child_env)
        self.assertNotIn('GH_TOKEN', client.child_env)
        self.assertEqual((Path(client.temp.name)/'identity').stat().st_mode & 0o777, 0o600)
        self.assertEqual(client.command[-1], 'receive')

    def test_client_wire_protocol_delivers_then_observes_worker_success(self):
        binary, web = self.inputs()
        artifact = self.root / 'release.tar'
        p.package(identity(), binary, web, artifact, verify_checkout=False)
        receiver = r.Receiver('dev', self.root / 'dev', self.root / 'state', Service(), lambda: None)
        receiver.register(identity())
        def ssh(command, input, **kwargs):
            line, body = input.split(b'\n', 1)
            request = json.loads(line)
            if request['operation'] == 'status':
                state = receiver.apply()
            elif request['operation'] == 'upload':
                state = receiver.upload(request, io.BytesIO(body))
            else:
                raise AssertionError('unexpected protocol operation')
            return subprocess.CompletedProcess(command, 0, json.dumps(state).encode(), b'')
        client = self.client()
        with patch.object(p.subprocess, 'run', side_effect=ssh), patch.object(p.time, 'sleep'):
            self.assertEqual(client.publish(identity(), artifact, check_head=False)['result'], 'success')

    def test_remote_failure_is_not_converted_to_success_or_skip(self):
        client = self.client()
        for payload, code in [({'error':'superseded'},1), ({'error':'archive_digest_mismatch'},1),
                              ({'result':'failed_rolled_back'},1), ({'result':'rollback_failed'},1)]:
            result = subprocess.CompletedProcess(client.command, code, json.dumps(payload).encode(), b'private diagnostics')
            with patch.object(p.subprocess, 'run', return_value=result):
                if 'error' in payload:
                    with self.assertRaises(p.Rejected):
                        client.request({'operation':'status'})
                else:
                    self.assertEqual(client.request({'operation':'status'})['result'], payload['result'])
        with patch.object(p.subprocess, 'run', return_value=subprocess.CompletedProcess([],1,b'not JSON',b'private diagnostics')):
            with self.assertRaisesRegex(p.Rejected, 'ssh_request_failed'):
                client.request({'operation':'status'})

    def test_wrong_remote_head_blocks_publication(self):
        class Response(io.BytesIO):
            def __enter__(self):return self
            def __exit__(self,*args):self.close()
        with patch.dict(os.environ, {'GH_TOKEN':'CI_SYNTHETIC_TOKEN'}), patch.object(p.urllib.request, 'urlopen', return_value=Response(json.dumps({'object':{'sha':'f'*40}}).encode())):
            with self.assertRaisesRegex(p.Rejected, 'branch_head_changed'):
                p.verify_remote_head(identity())


if __name__ == '__main__':
    unittest.main()
