#!/usr/bin/env python3
"""Synthetic repositories only; no network, real credentials, or private data."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCANNER = Path(__file__).with_name('check-git-history.py')


class HistoryGateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='itmap-history-test-')
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name) / 'repo'
        self.repo.mkdir()
        self.git('init', '-q', '--initial-branch=main')
        self.git('config', 'user.name', 'Synthetic CI')
        self.git('config', 'user.email', 'ci@example.invalid')
        (self.repo / 'README.md').write_text('synthetic source')
        self.commit()

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args], stderr=subprocess.PIPE)

    def commit(self):
        self.git('add', '.')
        self.git('-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'synthetic checkpoint')

    def scan(self, repo=None):
        return subprocess.run([sys.executable, str(SCANNER), '--repo', str(repo or self.repo)],
                              capture_output=True, text=True)

    def test_clean_full_repository_passes(self):
        result = self.scan()
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn('1 commits, 1 unique reachable blobs', result.stdout)

    def test_deleted_token_still_blocks_without_printing_value(self):
        token = 'ghp_' + 'A' * 40
        path = self.repo / 'removed.txt'
        path.write_text(token)
        self.commit()
        path.unlink()
        self.commit()
        result = self.scan()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('github-token', result.stdout)
        self.assertNotIn(token, result.stdout + result.stderr)

    def test_removed_archive_path_still_blocks(self):
        path = self.repo / 'private-backup.zip'
        path.write_bytes(b'synthetic-not-an-archive')
        self.commit()
        path.unlink()
        self.commit()
        result = self.scan()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('archive-or-private-path', result.stdout)

    def test_template_allowed_but_private_env_blocks(self):
        (self.repo / '.env.example').write_text('SESSION_SECRET=\n')
        self.commit()
        self.assertEqual(self.scan().returncode, 0)
        (self.repo / '.ENV').write_text('synthetic placeholder')
        self.commit()
        result = self.scan()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('archive-or-private-path', result.stdout)

    def test_binary_token_is_not_skipped(self):
        (self.repo / 'image.bin').write_bytes(b'\xff\x00' + ('ghp_' + 'B' * 40).encode() + b'\x00')
        self.commit()
        result = self.scan()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('github-token', result.stdout)

    def test_shallow_clone_is_not_reported_as_complete(self):
        (self.repo / 'second.txt').write_text('second synthetic commit')
        self.commit()
        shallow = Path(self.tmp.name) / 'shallow'
        subprocess.run(['git', 'clone', '-q', '--depth=1', self.repo.as_uri(), str(shallow)],
                       check=True, capture_output=True)
        result = self.scan(shallow)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('shallow', result.stdout)


if __name__ == '__main__':
    unittest.main()
