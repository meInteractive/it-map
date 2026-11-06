#!/usr/bin/env python3
"""Один защищённый локальный сигнал диспетчеру в течение пилота."""
import json
import os
import sys
import time
import urllib.error
import urllib.request

URL = 'http://127.0.0.1:18080/api/internal/scheduler/tick'


class TickError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def run(env, now=None, opener=None, diagnostic=False):
    token = env.get('SCHEDULER_TOKEN', '')
    raw_until = env.get('ITMAP_SCHEDULER_UNTIL', '')
    if not token or len(token) > 1024 or '\n' in token or '\r' in token:
        raise TickError('scheduler_token_invalid')
    if not raw_until.isascii() or not raw_until.isdigit() or len(raw_until) > 12:
        raise TickError('pilot_deadline_invalid')
    deadline = int(raw_until)
    if (time.time() if now is None else now) >= deadline:
        return {'status': 'pilot_expired'}
    body = {'diagnostic': True} if diagnostic else {}
    request = urllib.request.Request(URL, data=json.dumps(body).encode(), method='POST', headers={
        'Authorization': 'Bearer ' + token,
        'Content-Type': 'application/json',
    })
    if opener is None:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect()).open
    try:
        with opener(request, timeout=130) as response:
            raw = response.read(65537)
        if len(raw) > 65536:
            raise TickError('response_too_large')
        result = json.loads(raw)
    except (OSError, ValueError, urllib.error.URLError) as exc:
        raise TickError('tick_request_failed') from exc
    if not isinstance(result, dict):
        raise TickError('tick_response_invalid')
    if diagnostic:
        job = result.get('job')
        if not isinstance(job, dict) or job.get('status') != 'completed' or result.get('externalRequests') != 0:
            raise TickError('diagnostic_failed')
        return {'status': 'diagnostic_ok'}
    status = result.get('status')
    processed, errors = result.get('processed'), result.get('errors')
    if status not in ('completed', 'partial', 'interrupted') or type(processed) is not int or type(errors) is not int or processed < 0 or errors < 0:
        raise TickError('tick_response_invalid')
    return {'status': status, 'processed': processed, 'errors': errors}


def main():
    if len(sys.argv) > 2 or len(sys.argv) == 2 and sys.argv[1] != '--diagnostic':
        raise TickError('argument_invalid')
    result = run(os.environ, diagnostic=len(sys.argv) == 2)
    print(json.dumps(result, ensure_ascii=False))
    return 1 if result.get('errors', 0) or result['status'] in ('partial', 'interrupted') else 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except TickError as error:
        print(json.dumps({'error': str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
