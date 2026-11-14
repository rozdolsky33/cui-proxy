"""Value-free upstream receipts; incomplete capture can never prove absence."""

import hashlib
import re
import threading


def receipt_id(operation):
    if not isinstance(operation, str) or not operation:
        raise ValueError("soak_receipt_identity")
    return hashlib.sha256(operation.encode()).hexdigest()


def _valid(key):
    return isinstance(key, str) and re.fullmatch("[0-9a-f]{64}", key) is not None


class ReceiptStore:
    def __init__(self, capacity=100000):
        if type(capacity) is not int or capacity < 1:
            raise ValueError("soak_receipt_capacity")
        self.capacity = capacity
        self.rows = {}
        self.complete = True
        self.lock = threading.Lock()

    def record(self, key, body):
        with self.lock:
            if not _valid(key):
                self.complete = False
                return
            if key not in self.rows and len(self.rows) >= self.capacity:
                self.complete = False
                return
            old = self.rows.get(key, {"count": 0, "active": 0, "finished": 0, "disconnected": 0})
            self.rows[key] = {"count": old["count"] + 1, "active": old["active"] + 1,
                              "finished": old["finished"], "disconnected": old["disconnected"],
                              "body_sha256": hashlib.sha256(body).hexdigest() if body is not None else None}

    def complete_body(self, key, body):
        with self.lock:
            if not _valid(key) or key not in self.rows:
                self.complete = False
                return
            self.rows[key]["body_sha256"] = hashlib.sha256(body).hexdigest()

    def get(self, key):
        if not _valid(key):
            raise ValueError("soak_receipt_identity")
        with self.lock:
            row = self.rows.get(key, {"count": 0, "body_sha256": None})
            return {"count": row["count"], "body_sha256": row["body_sha256"], "complete": self.complete}

    def finish(self, key, *, disconnected=False):
        with self.lock:
            if not _valid(key) or key not in self.rows:
                self.complete = False
                return
            row = self.rows[key]
            if row["active"] < 1:
                self.complete = False
                raise ValueError("soak_receipt_duplicate_finish")
            row["active"] -= 1
            row["finished"] += 1
            row["disconnected"] += int(disconnected)

    def activity(self, key):
        if not _valid(key):
            raise ValueError("soak_receipt_identity")
        with self.lock:
            row = self.rows.get(key, {"count": 0, "active": 0, "finished": 0, "disconnected": 0})
            return {"complete": self.complete, "arrived": row["count"],
                    "active": row["active"], "finished": row["finished"],
                    "disconnected": row["disconnected"]}


def verify_receipt(receipt, *, absent=False, expected_hash=None):
    if receipt.get("complete") is not True or type(receipt.get("count")) is not int:
        raise ValueError("soak_receipt_incomplete")
    if absent:
        if expected_hash is not None or receipt["count"] != 0 or receipt.get("body_sha256") is not None:
            raise ValueError("soak_receipt_unexpected_arrival")
    elif not _valid(expected_hash) or receipt["count"] != 1 or receipt.get("body_sha256") != expected_hash:
        raise ValueError("soak_receipt_boundary_mismatch")
    return True
