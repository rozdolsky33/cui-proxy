import contextlib
import io
import unittest

import churn_smoke
import performance_churn


class ChurnFailureMetadataTests(unittest.TestCase):
    def test_retains_exception_chain_without_messages(self):
        try:
            try:
                raise TypeError('private message must not be emitted')
            except TypeError as cause:
                raise RuntimeError('another private message') from cause
        except RuntimeError as error:
            result = performance_churn.failure_metadata(error)
        self.assertEqual(result['exception_types'], ['RuntimeError', 'TypeError'])
        self.assertNotIn('private message', repr(result))
        self.assertTrue(all(type(line) is int and line > 0 for line in result['workflow_lines']))

    def test_smoke_prints_only_validated_failure_metadata(self):
        report = {'failure_metadata': {'exception_types': ['TypeError'], 'workflow_lines': [17]}}
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            churn_smoke._metadata(report)
        self.assertIn('smoke_failure_types TypeError', output.getvalue())
        self.assertIn('smoke_failure_lines 17', output.getvalue())

    def test_smoke_ignores_untrusted_failure_metadata(self):
        report = {'failure_metadata': {'exception_types': ['untrusted_value'], 'workflow_lines': ['untrusted_value']}}
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            churn_smoke._metadata(report)
        self.assertNotIn('untrusted_value', output.getvalue())

    def test_report_writer_rejects_invalid_failure_metadata(self):
        import tempfile
        from pathlib import Path
        import performance
        for diagnostic in (
            {'exception_types': ['untrusted_value'], 'workflow_lines': [1]},
            {'exception_types': ['ValueError'], 'workflow_lines': [True]},
            {'exception_types': ['ValueError'], 'workflow_lines': [1], 'message': 'untrusted_value'},
        ):
            with self.subTest(diagnostic=diagnostic), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'report.json'
                with self.assertRaises(ValueError):
                    performance.write_report(path, {'failure_metadata': diagnostic})
                self.assertFalse(path.exists())

    def test_custom_exception_name_is_not_emitted(self):
        from workflow_failure_metadata import failure_metadata
        class UntrustedName(Exception):
            pass
        self.assertEqual(failure_metadata(UntrustedName('message'))['exception_types'], ['Exception'])


if __name__ == '__main__':
    unittest.main()
