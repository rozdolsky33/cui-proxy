"""Static-prepared regressions for real evaluator provenance and external read races."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import soak_growth_review as growth


# Runs only when root executes these tests after the reference lock is released.
# Real copied modules and real Git identity are used; only expensive campaign
# loading/evaluation is replaced with the established small review fixtures.
CLI_BOOTSTRAP = r"""
from contextlib import redirect_stdout
import hashlib
import io
import json
from pathlib import Path
import sys
from unittest.mock import patch

producer, workspace, mode = map(Path, sys.argv[1:4])
mode = str(mode)
sys.path.insert(0, str(producer / 'scripts' / 'workflow'))
import performance_campaign
import soak_acceptance_cli as cli
import soak_growth_review as growth
import test_soak_growth_review as review_cases

workspace.mkdir()
campaign_root = workspace / 'campaign'
campaign_root.mkdir()
fixture = review_cases.GrowthReviewTests()
fixture.setUp()
identity_path = workspace / 'identity.json'
identity_path.write_text(json.dumps(fixture.expected))
review_path = fixture.files(workspace)
output = workspace / 'acceptance.json'
helper_path = Path(growth.__file__).resolve()
helper_digest = hashlib.sha256(helper_path.read_bytes()).hexdigest()
if mode == 'missing-origin':
    del growth.__file__
elif mode == 'external-origin':
    external = workspace / 'external-helper.py'
    external.write_bytes(helper_path.read_bytes())
    growth.__file__ = str(external)
elif mode == 'changed-bytes':
    helper_path.write_bytes(helper_path.read_bytes() + b'\n# tracked bytes changed after import\n')

source = performance_campaign.source_identity()
files = {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
         for path in sorted((producer / 'scripts' / 'workflow').glob('*.py'))}
aggregate = hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()
metadata = {'aggregate_is_string': type(source.get('script_sha256')) is str,
    'aggregate_length': len(source.get('script_sha256', '')),
    'aggregate_matches_real_inventory': source.get('script_sha256') == aggregate,
    'source_clean': source.get('dirty') is False,
    'accepted': False, 'status': None, 'error_class': None,
    'reported_aggregate_unchanged': False, 'individual_helper_digest_recorded': False}

def evaluate(campaign, expected, *, growth_review=None):
    assert campaign == fixture.campaign
    assert expected == fixture.expected
    gate = fixture.evaluate(growth_review)
    return {'state': gate['state'], 'soak_qualified': gate['state'] == 'pass',
            'performance_qualified': False, 'gates': {'growth_review': gate}}

arguments = ['--root', str(campaign_root), '--expected-identity', str(identity_path),
    '--output', str(output), '--growth-review', str(review_path)]
try:
    with patch.object(cli, 'read_completed_campaign', return_value=fixture.campaign), \
         patch.object(cli, 'evaluate_completed_soak', side_effect=evaluate), \
         redirect_stdout(io.StringIO()):
        metadata['status'] = cli.main(arguments)
except Exception as error:
    metadata['status'] = 2
    metadata['error_class'] = type(error).__name__
if output.exists():
    report = json.loads(output.read_bytes())
    metadata['accepted'] = report.get('soak_qualified') is True
    recorded = report.get('evaluator_source', {})
    metadata['reported_aggregate_unchanged'] = recorded.get('script_sha256') == source['script_sha256']
    modules = recorded.get('module_sha256', {})
    metadata['individual_helper_digest_recorded'] = type(modules) is dict and modules.get('soak_growth_review.py') == helper_digest
print(json.dumps(metadata, sort_keys=True))
"""


class RealGrowthReviewProvenanceTests(unittest.TestCase):
    def run_real_cli(self, mode='valid'):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            producer = directory / 'producer'
            workflow = producer / 'scripts' / 'workflow'
            workflow.mkdir(parents=True)
            source = Path(__file__).resolve().parent
            for path in source.glob('*.py'):
                shutil.copyfile(path, workflow / path.name)
            shutil.copyfile(source / 'manifest.json', workflow / 'manifest.json')
            (producer / '.gitignore').write_text('__pycache__/\n')
            for command in (['git', '-c', 'init.templateDir=', 'init', '-q', str(producer)],
                    ['git', '-C', str(producer), 'add', '.'],
                    ['git', '-C', str(producer), '-c', 'commit.gpgsign=false',
                     '-c', 'core.hooksPath=/dev/null', '-c', 'user.name=Regression Fixture',
                     '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture']):
                initialized = subprocess.run(command, capture_output=True, timeout=30)
                self.assertEqual(initialized.returncode, 0, 'private Git fixture preparation failed')
            completed = subprocess.run([sys.executable, '-B', '-c', CLI_BOOTSTRAP,
                str(producer), str(directory / 'workspace'), mode],
                capture_output=True, timeout=45)
            self.assertEqual(completed.returncode, 0, 'closed child metadata unavailable')
            metadata = json.loads(completed.stdout)
            self.assertTrue(metadata['aggregate_is_string'])
            self.assertEqual(metadata['aggregate_length'], 64)
            self.assertTrue(metadata['aggregate_matches_real_inventory'])
            return metadata

    def test_real_source_identity_preserves_aggregate_string_contract(self):
        metadata = self.run_real_cli()
        self.assertTrue(metadata['source_clean'])
        self.assertTrue(metadata['aggregate_is_string'])
        self.assertTrue(metadata['aggregate_matches_real_inventory'])

    def test_valid_review_cli_accepts_real_clean_source_and_actual_imported_helper(self):
        metadata = self.run_real_cli()
        self.assertTrue(metadata['source_clean'])
        self.assertEqual(metadata['status'], 0, metadata['error_class'])
        self.assertTrue(metadata['accepted'])
        self.assertTrue(metadata['reported_aggregate_unchanged'])
        self.assertTrue(metadata['individual_helper_digest_recorded'])

    def test_missing_or_untracked_imported_helper_origin_cannot_qualify(self):
        for mode in ('missing-origin', 'external-origin'):
            with self.subTest(mode=mode):
                metadata = self.run_real_cli(mode)
                self.assertTrue(metadata['source_clean'])
                self.assertNotEqual(metadata['status'], 0)
                self.assertFalse(metadata['accepted'])

    def test_changed_actual_helper_bytes_cannot_qualify_clean_provenance(self):
        metadata = self.run_real_cli('changed-bytes')
        self.assertFalse(metadata['source_clean'])
        self.assertNotEqual(metadata['status'], 0)
        self.assertFalse(metadata['accepted'])


class ExternalReviewParentSwapTests(unittest.TestCase):
    def test_parent_symlink_swap_never_reads_campaign_internal_review_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve()
            root = directory / 'campaign'
            internal = root / 'inside'
            internal.mkdir(parents=True)
            parent = directory / 'external' / 'parent'
            parent.mkdir(parents=True)
            bound_parent = parent.with_name('bound-parent')
            name = 'review.json'
            outside_bytes = b'external-review'
            inside_bytes = b'internal-review'
            (parent / name).write_bytes(outside_bytes)
            (internal / name).write_bytes(inside_bytes)
            original_open = growth.os.open
            swapped = False
            def open_with_barrier(path, flags, *args, **kwargs):
                nonlocal swapped
                if not swapped and Path(os.fsdecode(path)).name == name:
                    parent.rename(bound_parent)
                    parent.symlink_to(internal, target_is_directory=True)
                    swapped = True
                return original_open(path, flags, *args, **kwargs)
            returned = None
            rejected = False
            with patch.object(growth.os, 'open', side_effect=open_with_barrier):
                try:
                    returned = growth._capture(parent / name, 1024, root)
                except (ValueError, OSError):
                    rejected = True
            self.assertTrue(swapped, 'final-open interleaving barrier was not exercised')
            self.assertTrue(rejected or returned == outside_bytes)
            self.assertNotEqual(returned, inside_bytes)
