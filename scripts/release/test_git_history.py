"""Check the two-release history assumption with real local Git operations."""
import os
import subprocess
import tempfile
from pathlib import Path
from unittest import TestCase


class ReleaseHistoryTest(TestCase):
    def test_two_merge_commits_preserve_history_and_snapshot(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            env = os.environ | {'GIT_AUTHOR_NAME': 'Test', 'GIT_AUTHOR_EMAIL': 'test@example.invalid',
                                'GIT_COMMITTER_NAME': 'Test', 'GIT_COMMITTER_EMAIL': 'test@example.invalid'}
            def git(*args):
                return subprocess.run(['git', *args], cwd=root, env=env, check=True,
                                      capture_output=True, text=True).stdout.strip()
            def commit(path, content):
                (root / path).write_text(content)
                git('add', path)
                git('commit', '-m', content)
                return git('rev-parse', 'HEAD')
            git('init', '-b', 'main')
            commit('base', 'baseline')
            git('switch', '-c', 'develop')
            first = commit('first', 'first change')
            git('branch', 'deploy/2026-11-26.1')
            second = commit('second', 'second change')
            git('switch', 'main')
            git('merge', '--no-ff', '-m', 'first release', 'deploy/2026-11-26.1')
            self.assertEqual(git('rev-parse', 'HEAD^2'), first)
            self.assertFalse((root / 'second').exists())
            self.assertEqual(git('log', '--format=%s', 'HEAD..develop'), 'second change')
            git('branch', 'deploy/2026-11-26.2', second)
            git('merge', '--no-ff', '-m', 'second release', 'deploy/2026-11-26.2')
            self.assertEqual(git('rev-parse', 'HEAD^2'), second)
            self.assertEqual(git('log', '--format=%s', 'HEAD..develop'), '')
            self.assertEqual((root / 'second').read_text(), 'second change')
