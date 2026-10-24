#!/usr/bin/env python3
"""Read-only audit of all reachable refs; never print blob contents or values."""
import argparse
from pathlib import Path
import re
import subprocess
import sys

from public_repo_rules import TOKEN_RULES


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], stderr=subprocess.PIPE)


def forbidden_path(name):
    parts = name.split('/')
    if any(part.lower().startswith(('.env', '.dev.vars')) and part not in ('.env.example', '.dev.vars.example') for part in parts):
        return True
    if name.lower().startswith(('dist/', '.openai/', '.vinext/', 'worker/', 'drizzle/')):
        return True
    if name.lower().endswith(('.zip', '.bundle', '.sqlite', '.sqlite3', '.db', '.dump', '.bak', '.pem', '.key', '.p12', '.pfx', '.sql.gz')):
        return True
    return name != 'server/generated/backup-schema.json' and bool(re.search(
        r'(?i)(backup|snapshot|d1-original|mariadb).*(\.json|\.sql|\.gz)$', name))


def audit(repo):
    if git(repo, 'rev-parse', '--is-shallow-repository').strip() != b'false':
        raise ValueError('shallow repository: fetch full history before auditing')
    git(repo, 'rev-parse', '--verify', 'HEAD^{commit}')
    commits = git(repo, 'rev-list', '--all').splitlines()
    objects = git(repo, 'rev-list', '--objects', '--all', '--no-object-names').splitlines()
    if not commits or not objects:
        raise ValueError('no reachable committed history to audit')
    issues = set()
    # Inspect every historical tree: deleting a file does not erase its history.
    for commit in commits:
        for entry in git(repo, 'ls-tree', '-r', '-z', '--full-tree', commit.decode()).split(b'\0'):
            if not entry:
                continue
            metadata, path = entry.split(b'\t', 1)
            if forbidden_path(path.decode('utf-8', errors='surrogateescape')):
                issues.add((metadata.split()[2].decode(), 'archive-or-private-path'))
    rules = {name: re.compile(pattern.encode('ascii')) for name, pattern in TOKEN_RULES.items()}
    blobs = 0
    # Binary blobs are scanned too; no decoding or filename-based exclusions.
    process = subprocess.Popen(['git', '-C', str(repo), 'cat-file', '--batch'],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    try:
        for oid in objects:
            process.stdin.write(oid + b'\n')
            process.stdin.flush()
            header = process.stdout.readline().split()
            if len(header) != 3 or header[0] != oid:
                raise ValueError('unreadable Git object')
            size = int(header[2])
            payload = process.stdout.read(size)
            if len(payload) != size or process.stdout.read(1) != b'\n':
                raise ValueError('truncated Git object')
            if header[1] != b'blob':
                continue
            blobs += 1
            for name, rule in rules.items():
                if rule.search(payload):
                    issues.add((oid.decode(), name))
        process.stdin.close()
        if process.wait() != 0:
            raise ValueError('Git object reader failed')
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        process.stdout.close()
    for oid, rule in sorted(issues):
        # Object hashes identify findings without exposing names or secret values.
        print(f'BLOCKED: history object {oid} ({rule})')
    if issues:
        return 1
    print(f'PASS: {len(commits)} commits, {blobs} unique reachable blobs; known token/path rules only')
    return 0


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        sys.exit(audit(args.repo))
    except (subprocess.CalledProcessError, OSError, ValueError) as error:
        message = str(error) if isinstance(error, ValueError) else 'Git history unavailable'
        print(f'FAIL: {message}')
        sys.exit(1)
