"""Exercise capture-path validation through the actual loopback upstream."""

import http.client
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

import harness


class CapturePathTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="cui-capture-path-test-")
        cls.addClassCleanup(cls.temp.cleanup)
        cls.directory = Path(cls.temp.name)
        cls.process = subprocess.Popen(
            [sys.executable, str(Path(harness.__file__).resolve()),
             "--upstream", cls.temp.name],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        cls.addClassCleanup(cls.stop)
        ready = cls.directory / "ready.json"
        harness.wait_until(ready.exists, 5, "test_upstream_start")
        cls.port = json.loads(ready.read_text())["port"]

    @classmethod
    def stop(cls):
        cls.process.terminate()
        try:
            cls.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            cls.process.kill()
            cls.process.wait(timeout=5)

    def request(self, ident, fault="upstream"):
        connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=3)
        self.addCleanup(connection.close)
        body = json.dumps({"metadata": {"harness_id": ident, "fault": fault},
                           "input": []}).encode()
        connection.request("POST", "/v1/responses", body,
                           {"Content-Type": "application/json"})
        try:
            response = connection.getresponse()
        except http.client.RemoteDisconnected:
            return None
        response.read()
        return response.status

    def test_invalid_capture_ids_are_rejected_without_writes(self):
        invalid = (None, 7, [], {}, "", "-", "../outside", "..\\outside",
                   "/absolute", "nested/path", "embedded\x00nul", "line\nbreak",
                   "unicode-é", "a" * 129, "percent%2fpath", "dot.name")
        for ident in invalid:
            with self.subTest(case_type=type(ident).__name__):
                before = set(self.directory.iterdir())
                self.assertEqual(self.request(ident), 400)
                self.assertEqual(set(self.directory.iterdir()), before)

    def test_valid_capture_ids_keep_expected_filenames(self):
        for ident in ("case-1", "ABC123", "b" * 128):
            with self.subTest(length=len(ident)):
                self.assertEqual(self.request(ident), 503)
                path = self.directory / (ident + ".json")
                self.assertTrue(path.is_file())
                self.assertEqual(path.parent.resolve(), self.directory.resolve())

    def test_invalid_cancellation_id_cannot_create_marker(self):
        before = set(self.directory.iterdir())
        self.assertEqual(self.request("unicode-é", "cancel"), 400)
        time.sleep(0.05)
        self.assertEqual(set(self.directory.iterdir()), before)
