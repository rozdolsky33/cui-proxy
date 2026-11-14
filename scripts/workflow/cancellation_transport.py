"""Thread-scoped HTTP instrumentation for the isolated cancellation probe."""
from contextlib import contextmanager
import http.client
import socket
import threading
import time


class ConnectionTracker:
    def __init__(self, host, port, *, module=http.client, clock=time.monotonic):
        self.host, self.port = host, port
        self.module, self.clock = module, clock
        self._lock = threading.RLock()
        self._local = threading.local()
        self._records = {}
        self._active = False
        self._entered = False

    def __enter__(self):
        if self._entered or getattr(self.module.HTTPConnection, "_cancellation_tracker", False):
            raise ValueError("cancellation_transport_owner")
        self._entered = True
        self._original = self.module.HTTPConnection
        owner = self

        class TrackedConnection(self._original):
            _cancellation_tracker = True

            def __init__(self, *args, **kwargs):
                super().__init__(*args, **kwargs)
                self._probe_record = None
                identity = getattr(owner._local, "identity", None)
                if identity is None or (self.host, self.port) != (owner.host, owner.port):
                    return
                with owner._lock:
                    if not owner._active:
                        raise ValueError("cancellation_transport_inactive")
                    record = owner._records[identity]
                    if record["connection"] is not None:
                        raise ValueError("cancellation_transport_duplicate")
                    record["connection"] = self
                    self._probe_record = record

            def connect(self):
                super().connect()
                if self._probe_record is not None:
                    with owner._lock:
                        if not owner._active:
                            self.close()
                            raise ValueError("cancellation_transport_inactive")
                        self._probe_record["connected_s"] = owner.clock()

            def getresponse(self):
                response = super().getresponse()
                if self._probe_record is not None:
                    with owner._lock:
                        self._probe_record["headers_s"] = owner.clock()
                return response

            def close(self):
                try:
                    super().close()
                finally:
                    record = getattr(self, "_probe_record", None)
                    if record is not None:
                        with owner._lock:
                            if record["transport_end_s"] is None:
                                record["transport_end_s"] = owner.clock()

        self._installed = TrackedConnection
        self._active = True
        self.module.HTTPConnection = TrackedConnection
        return self

    @contextmanager
    def track(self, identity):
        with self._lock:
            if (not self._active or getattr(self._local, "identity", None) is not None
                    or not isinstance(identity, str) or not identity.strip()
                    or not identity.isprintable() or len(identity) > 128
                    or identity in self._records):
                raise ValueError("cancellation_transport_scope")
            self._records[identity] = {
                "connection": None,
                "request_started_s": self.clock(),
                "connected_s": None,
                "headers_s": None,
                "cancel_s": None,
                "transport_end_s": None,
                "closed_by_probe": False,
                "headers_received_before_cancel": False,
            }
            self._local.identity = identity
        try:
            yield
        finally:
            self._local.identity = None

    def snapshot(self, identity):
        with self._lock:
            if identity not in self._records:
                raise ValueError("cancellation_transport_missing")
            return {key: value for key, value in self._records[identity].items()
                    if key != "connection"}

    def cancel(self, identity):
        with self._lock:
            if not self._active or identity not in self._records:
                raise ValueError("cancellation_transport_missing")
            record = self._records[identity]
            connection = record["connection"]
            if (connection is None or record["connected_s"] is None
                    or record["cancel_s"] is not None or record["headers_s"] is not None
                    or record["transport_end_s"] is not None or connection.sock is None):
                raise ValueError("cancellation_transport_not_pending")
            record["cancel_s"] = self.clock()
            # Shutdown wakes the worker blocked in getresponse/read. Do not call
            # connection.close here: its worker-side close is the observed end.
            connection.sock.shutdown(socket.SHUT_RDWR)
            record["closed_by_probe"] = True
            return self.snapshot(identity)

    def __exit__(self, kind, error, traceback):
        with self._lock:
            self._active = False
            connections = [row["connection"] for row in self._records.values()
                           if row["connection"] is not None]
        cleanup_error = None
        try:
            for connection in connections:
                try:
                    if connection.sock is not None:
                        try:
                            connection.sock.shutdown(socket.SHUT_RDWR)
                        except OSError:
                            pass
                    connection.close()
                except Exception as failure:
                    if cleanup_error is None:
                        cleanup_error = failure
        finally:
            if self.module.HTTPConnection is self._installed:
                self.module.HTTPConnection = self._original
            else:
                cleanup_error = ValueError("cancellation_transport_owner_changed")
        if cleanup_error is not None and error is None:
            raise cleanup_error
        return False
