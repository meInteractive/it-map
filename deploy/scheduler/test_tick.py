import importlib.util
import io
import json
import unittest
from urllib.error import HTTPError
from pathlib import Path

spec = importlib.util.spec_from_file_location('itmap_scheduler_tick', Path(__file__).with_name('tick.py'))
tick = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tick)


class Response:
    def __init__(self, value):
        self.body = io.BytesIO(json.dumps(value).encode())
    def __enter__(self):
        return self
    def __exit__(self, *_):
        return False
    def read(self, limit):
        return self.body.read(limit)


class TickTests(unittest.TestCase):
    def test_hourly_request_and_redacted_output(self):
        calls = []
        def open_request(request, timeout):
            calls.append((request, timeout))
            return Response({'status': 'completed', 'processed': 2, 'errors': 0, 'sensitive': 'CI_SECRET'})
        result = tick.run({'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': '200'}, now=100, opener=open_request)
        self.assertEqual(result, {'status': 'completed', 'processed': 2, 'errors': 0})
        self.assertEqual(calls[0][0].full_url, 'http://127.0.0.1:18080/api/internal/scheduler/tick')
        self.assertEqual(calls[0][0].get_header('Authorization'), 'Bearer CI_TOKEN')
        self.assertEqual(json.loads(calls[0][0].data), {})
        self.assertNotIn('CI_SECRET', json.dumps(result))

    def test_independent_profiles_are_idempotent_per_minute(self):
        calls = []
        def opener(req, timeout):
            calls.append(json.loads(req.data))
            return Response({'status': 'completed', 'processed': 0, 'errors': 0})
        env = {'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': '1000'}
        for moment in (120, 125, 180):
            self.assertEqual(tick.run(env, now=moment, opener=opener, profile='reconcile')['status'], 'completed')
        self.assertEqual(calls, [
            {'requestId': 'minute:2', 'profile': 'reconcile'},
            {'requestId': 'minute:2', 'profile': 'reconcile'},
            {'requestId': 'minute:3', 'profile': 'reconcile'},
        ])
        with self.assertRaises(tick.TickError):
            tick.run(env, now=200, opener=opener, profile='unknown')
        with self.assertRaises(tick.TickError):
            tick.run(env, now=200, opener=opener, diagnostic=True, profile='launch')

    def test_expired_or_missing_configuration_never_calls_server(self):
        def forbidden(*args, **kwargs):
            raise AssertionError('unexpected request')
        self.assertEqual(tick.run({'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': '100'}, now=100, opener=forbidden), {'status': 'pilot_expired'})
        for env in ({}, {'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': 'bad'}, {'SCHEDULER_TOKEN': 'x\ny', 'ITMAP_SCHEDULER_UNTIL': '200'}):
            with self.assertRaises(tick.TickError):
                tick.run(env, now=100, opener=forbidden)

    def test_diagnostic_requires_no_provider_work(self):
        def open_request(request, timeout):
            self.assertEqual(json.loads(request.data), {'diagnostic': True})
            return Response({'job': {'status': 'completed'}, 'externalRequests': 0})
        self.assertEqual(tick.run({'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': '200'}, now=100, opener=open_request, diagnostic=True), {'status': 'diagnostic_ok'})
        with self.assertRaises(tick.TickError):
            tick.run({'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': '200'}, now=100, opener=lambda *a, **kw: Response({'job': {'status': 'completed'}, 'externalRequests': 1}), diagnostic=True)

    def test_provider_failure_is_redacted(self):
        def failure(request, timeout):
            raise HTTPError(request.full_url, 503, 'CI_SECRET', {}, None)
        with self.assertRaises(tick.TickError) as result:
            tick.run({'SCHEDULER_TOKEN': 'CI_TOKEN', 'ITMAP_SCHEDULER_UNTIL': '200'}, now=100, opener=failure)
        self.assertNotIn('CI_SECRET', str(result.exception))

    def test_redirect_is_never_followed(self):
        self.assertIsNone(tick.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://example.invalid/'))

if __name__ == '__main__':
    unittest.main()
