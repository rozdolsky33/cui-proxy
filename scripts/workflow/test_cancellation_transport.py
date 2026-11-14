import threading
from types import SimpleNamespace
import unittest

from cancellation_transport import ConnectionTracker


class FakeSocket:
    def __init__(self):
        self.shutdowns = []

    def shutdown(self, direction):
        self.shutdowns.append(direction)


class FakeConnection:
    def __init__(self, host, port=None, **kwargs):
        self.host, self.port = host, port
        self.sock = None
        self.closed = 0

    def connect(self):
        self.sock = FakeSocket()

    def getresponse(self):
        return object()

    def close(self):
        self.closed += 1
        self.sock = None


class ConnectionTrackerTests(unittest.TestCase):
    def setUp(self):
        self.module = SimpleNamespace(HTTPConnection=FakeConnection)
        self.now = 10.0
        self.tracker = ConnectionTracker("127.0.0.1", 12345, module=self.module, clock=lambda: self.now)

    def connection(self, port=12345):
        connection = self.module.HTTPConnection("127.0.0.1", port, timeout=45)
        connection.connect()
        return connection

    def test_cancel_only_shuts_down_target_and_waits_for_worker_close(self):
        with self.tracker:
            unrelated = self.connection()
            with self.tracker.track("target"):
                target = self.connection()
                sock = target.sock
                self.now = 11.0
                state = self.tracker.cancel("target")
                self.assertEqual(len(sock.shutdowns), 1)
                self.assertEqual(unrelated.sock.shutdowns, [])
                self.assertEqual(state["cancel_s"], 11.0)
                self.assertTrue(state["closed_by_probe"])
                self.assertIsNone(state["transport_end_s"])
                self.now = 11.2
                target.close()
                self.assertEqual(self.tracker.snapshot("target")["transport_end_s"], 11.2)

    def test_other_origin_and_untagged_thread_are_excluded(self):
        with self.tracker:
            with self.tracker.track("target"):
                other_origin = self.connection(12346)
                created = []
                thread = threading.Thread(target=lambda: created.append(self.connection()))
                thread.start()
                thread.join(timeout=1)
                self.assertFalse(thread.is_alive())
                target = self.connection()
                self.tracker.cancel("target")
                self.assertEqual(other_origin.sock.shutdowns, [])
                self.assertEqual(created[0].sock.shutdowns, [])
                self.assertEqual(len(target.sock.shutdowns), 1)
                other_origin.close()
                created[0].close()

    def test_headers_before_cancel_are_rejected(self):
        with self.tracker:
            with self.tracker.track("target"):
                target = self.connection()
                target.getresponse()
                with self.assertRaises(ValueError):
                    self.tracker.cancel("target")

    def test_missing_unconnected_duplicate_and_late_cancels_are_rejected(self):
        with self.tracker:
            with self.assertRaises(ValueError):
                self.tracker.cancel("missing")
            with self.tracker.track("target"):
                target = self.module.HTTPConnection("127.0.0.1", 12345)
                with self.assertRaises(ValueError):
                    self.tracker.cancel("target")
                target.connect()
                self.tracker.cancel("target")
                with self.assertRaises(ValueError):
                    self.tracker.cancel("target")
            with self.tracker.track("late"):
                target = self.connection()
                target.close()
                with self.assertRaises(ValueError):
                    self.tracker.cancel("late")

    def test_scope_and_factory_restore_on_error_and_cleanup_connections(self):
        with self.assertRaises(RuntimeError):
            with self.tracker:
                with self.tracker.track("target"):
                    target = self.connection()
                    sock = target.sock
                    raise RuntimeError("intentional failure")
        self.assertIs(self.module.HTTPConnection, FakeConnection)
        self.assertIsNone(target.sock)
        self.assertEqual(len(sock.shutdowns), 1)
        with self.assertRaises(ValueError):
            self.tracker.cancel("target")

    def test_rejects_repeated_scope_and_multiple_connections_per_operation(self):
        with self.tracker:
            with self.tracker.track("target"):
                self.connection()
                with self.assertRaises(ValueError):
                    self.connection()
                with self.assertRaises(ValueError):
                    with self.tracker.track("nested"):
                        pass
            with self.assertRaises(ValueError):
                with self.tracker.track("target"):
                    pass

    def test_rejects_nested_tracker_without_corrupting_owner(self):
        with self.tracker:
            owned = self.module.HTTPConnection
            with self.assertRaises(ValueError):
                with ConnectionTracker("127.0.0.1", 12345, module=self.module):
                    pass
            self.assertIs(self.module.HTTPConnection, owned)
        self.assertIs(self.module.HTTPConnection, FakeConnection)

    def test_snapshot_is_detached_and_repeated_close_keeps_first_end(self):
        with self.tracker:
            with self.tracker.track("target"):
                target = self.connection()
                self.now = 12.0
                target.close()
                first = self.tracker.snapshot("target")
                first["transport_end_s"] = 99
                self.now = 13.0
                target.close()
                self.assertEqual(self.tracker.snapshot("target")["transport_end_s"], 12.0)


if __name__ == "__main__":
    unittest.main()
