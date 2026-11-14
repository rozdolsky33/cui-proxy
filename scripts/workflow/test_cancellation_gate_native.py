"""Native launcher contract checks; capture every child output privately."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

from cancellation_gate import GatedOCR


class NativeGatedOCRTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.delegate = self.root / "delegate"
        self.delegate.write_text(
            "#!" + sys.executable + "\n"
            "import hashlib,json,os,sys\n"
            "data=sys.stdin.buffer.read() if sys.argv[1:2]==['stdin'] else b''\n"
            "print(json.dumps(dict(pid=os.getpid(),size=len(data),sha256=hashlib.sha256(data).hexdigest(),args=sys.argv[1:])))\n"
        )
        self.delegate.chmod(0o700)
        self.environment = {"LC_ALL": "C", "OMP_THREAD_LIMIT": "1"}

    def start(self, gate, arguments, data=b"", oversized=False):
        source = tempfile.TemporaryFile()
        self.addCleanup(source.close)
        if oversized:
            source.truncate(80 * 1024 * 1024 + 1)
        else:
            source.write(data)
        source.seek(0)
        child = subprocess.Popen(
            [str(gate.launcher), *arguments], stdin=source,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=self.environment,
        )
        self.addCleanup(self.stop, child)
        return child

    @staticmethod
    def stop(child):
        if child.poll() is None:
            child.kill()
        child.communicate(timeout=5)

    def held(self, gate, child):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            markers = gate.snapshot()
            if markers:
                self.assertEqual(len(markers), 1)
                self.assertEqual(markers[0]["pid"], child.pid)
                self.assertIsNone(child.poll())
                return markers[0]
            self.assertIsNone(child.poll(), "launcher exited before admission")
            time.sleep(0.01)
        self.fail("launcher did not publish admission")

    def result(self, child):
        output, errors = child.communicate(timeout=5)
        self.assertEqual(child.returncode, 0)
        self.assertEqual(errors, b"")
        return json.loads(output)

    def test_version_delegates_immediately_even_when_armed(self):
        with GatedOCR(self.root / "gate", self.delegate) as gate:
            gate.arm()
            child = self.start(gate, ["--version"])
            result = self.result(child)
            self.assertEqual(result["args"], ["--version"])
            self.assertEqual(result["pid"], child.pid)
            self.assertEqual(gate.snapshot(), [])

    def test_unarmed_launcher_preserves_stdin_without_markers(self):
        data = bytes(range(256)) * 1024
        with GatedOCR(self.root / "gate", self.delegate) as gate:
            child = self.start(gate, ["stdin", "stdout", "-l", "eng"], data)
            result = self.result(child)
            self.assertEqual(result["size"], len(data))
            self.assertEqual(result["sha256"], hashlib.sha256(data).hexdigest())
            self.assertEqual(result["pid"], child.pid)

    def test_armed_launcher_preserves_binary_stdin_and_exec_identity(self):
        data = bytes(range(256)) * 4096
        with GatedOCR(self.root / "gate", self.delegate) as gate:
            generation = gate.arm()
            child = self.start(gate, ["stdin", "stdout", "-l", "eng"], data)
            marker = self.held(gate, child)
            self.assertEqual(marker["generation"], generation.name)
            self.assertEqual(marker["input_sha256"], hashlib.sha256(data).hexdigest())
            gate.release()
            result = self.result(child)
            self.assertEqual(result["size"], len(data))
            self.assertEqual(result["sha256"], marker["input_sha256"])
            self.assertEqual(result["pid"], child.pid)
            self.assertEqual(result["args"], ["stdin", "stdout", "-l", "eng"])

    def test_context_error_releases_held_helper(self):
        with self.assertRaises(RuntimeError):
            with GatedOCR(self.root / "gate", self.delegate) as gate:
                gate.arm()
                child = self.start(gate, ["stdin", "stdout"], b"release probe")
                self.held(gate, child)
                raise RuntimeError("intentional context exit")
        result = self.result(child)
        self.assertEqual(result["pid"], child.pid)
        self.assertEqual(result["size"], len(b"release probe"))

    def test_oversized_stdin_fails_before_marker_or_delegation(self):
        with GatedOCR(self.root / "gate", self.delegate) as gate:
            gate.arm()
            child = self.start(gate, ["stdin", "stdout"], oversized=True)
            output, errors = child.communicate(timeout=10)
            self.assertNotEqual(child.returncode, 0)
            self.assertEqual(output, b"")
            self.assertEqual(gate.snapshot(), [])

    def test_killed_held_launcher_never_delegates_after_release(self):
        with GatedOCR(self.root / "gate", self.delegate) as gate:
            gate.arm()
            child = self.start(gate, ["stdin", "stdout"], b"bounded native gate probe")
            self.held(gate, child)
            child.kill()
            output, errors = child.communicate(timeout=5)
            self.assertLess(child.returncode, 0)
            self.assertEqual(output, b"")
            self.assertEqual(errors, b"")
            gate.release()
            self.assertIsNotNone(child.poll())


if __name__ == "__main__":
    unittest.main()
