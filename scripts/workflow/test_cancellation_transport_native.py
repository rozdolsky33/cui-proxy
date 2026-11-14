"""Loopback-only proof that shutdown wakes a worker awaiting HTTP headers."""
import http.client
import socketserver
import threading
import unittest

from cancellation_transport import ConnectionTracker


class NativeConnectionTrackerTests(unittest.TestCase):
    def test_shutdown_unblocks_header_wait_and_worker_records_transport_end(self):
        accepted = threading.Event()
        release = threading.Event()
        finished = threading.Event()
        outcomes = []

        class Handler(socketserver.StreamRequestHandler):
            def handle(self):
                self.connection.settimeout(5)
                while self.rfile.readline(8192) not in (b"\r\n", b"", b"\n"):
                    pass
                accepted.set()
                release.wait(5)

        server = socketserver.TCPServer(("127.0.0.1", 0), Handler)
        serving = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.02}, daemon=True)
        serving.start()
        worker = None
        original = http.client.HTTPConnection
        try:
            with ConnectionTracker(*server.server_address) as tracker:
                def request():
                    connection = None
                    try:
                        with tracker.track("target"):
                            connection = http.client.HTTPConnection(*server.server_address, timeout=5)
                            connection.connect()
                            connection.request("GET", "/")
                            connection.getresponse()
                            outcomes.append("unexpected_headers")
                    except (OSError, http.client.HTTPException):
                        outcomes.append("transport_interrupted")
                    finally:
                        if connection is not None:
                            connection.close()
                        finished.set()

                worker = threading.Thread(target=request, daemon=True)
                worker.start()
                self.assertTrue(accepted.wait(3), "server did not receive request")
                state = tracker.cancel("target")
                self.assertTrue(state["closed_by_probe"])
                self.assertTrue(finished.wait(3), "shutdown did not wake request worker")
                state = tracker.snapshot("target")
                self.assertGreaterEqual(state["transport_end_s"], state["cancel_s"])
                self.assertIsNone(state["headers_s"])
                self.assertEqual(outcomes, ["transport_interrupted"])
        finally:
            release.set()
            server.shutdown()
            server.server_close()
            serving.join(timeout=5)
            if worker is not None:
                worker.join(timeout=5)
        self.assertIs(http.client.HTTPConnection, original)
        self.assertFalse(serving.is_alive())
        self.assertFalse(worker.is_alive())


if __name__ == "__main__":
    unittest.main()
