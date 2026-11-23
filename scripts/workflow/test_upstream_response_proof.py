import json
import unittest

import harness


class UpstreamResponseProofTests(unittest.TestCase):
    def wire(self):
        payload = json.dumps([harness.message("alias-only-token")])
        events = [{"type": "response.output_text.delta", "item_id": "item-output", "output_index": 0,
                   "delta": payload}, {"type": "response.completed"}]
        return b"".join(b"data: " + json.dumps(event).encode() + b"\n\n" for event in events)

    def test_complete_actual_write_proof_is_metadata_only_and_discards_response_bytes(self):
        proof = harness.UpstreamResponseProof(max_bytes=4096)
        wire = self.wire()
        for index in range(0, len(wire), 7):
            chunk = wire[index:index + 7]
            proof.record(chunk, len(chunk))
        metadata = proof.finish()
        self.assertTrue(metadata["completed"])
        self.assertEqual(metadata["bytes"], len(wire))
        self.assertEqual(metadata["semantic_sha256"], harness.semantic_sha256([harness.message("alias-only-token")]))
        self.assertEqual(proof.buffered_bytes, 0)
        self.assertNotIn("alias-only-token", json.dumps(metadata))

    def test_overflow_partial_write_and_incomplete_stream_clear_memory_and_cannot_qualify(self):
        for action in ("overflow", "partial", "incomplete"):
            proof = harness.UpstreamResponseProof(max_bytes=4 if action == "overflow" else 4096)
            with self.subTest(action=action), self.assertRaises(harness.CheckFailure):
                if action == "overflow":
                    proof.record(b"too large", 9)
                elif action == "partial":
                    proof.record(self.wire(), 1)
                else:
                    proof.record(b"data: {}\n\n", 10)
                    proof.finish()
            self.assertEqual(proof.buffered_bytes, 0)
            with self.assertRaises(harness.CheckFailure):
                proof.finish()

    def test_discard_removes_buffer_on_exception_and_default_capture_is_disabled(self):
        proof = harness.UpstreamResponseProof(max_bytes=4096)
        wire = self.wire()
        proof.record(wire[:10], 10)
        proof.discard()
        self.assertEqual(proof.buffered_bytes, 0)
        with self.assertRaises(harness.CheckFailure):
            proof.finish()
        environment = harness.Environment("unused-test-binary")
        self.assertFalse(environment.response_proof)

    def test_unknown_or_ignored_sse_fields_never_receive_a_passing_digest(self):
        event = {"type": "response.output_text.delta", "item_id": "item-output", "output_index": 0,
                 "delta": "[]", "ignored": "must not be silently discarded"}
        wire = b"data: " + json.dumps(event).encode() + b"\n\ndata: {\"type\":\"response.completed\"}\n\n"
        proof = harness.UpstreamResponseProof(max_bytes=4096)
        proof.record(wire, len(wire))
        with self.assertRaises(harness.CheckFailure):
            proof.finish()
        self.assertEqual(proof.buffered_bytes, 0)

    def test_duplicate_sse_or_nested_payload_keys_and_nonfinite_values_clear_memory(self):
        terminal = b'data: {"type":"response.completed"}\n\n'
        duplicate_event = (b'data: {"type":"response.output_text.delta","item_id":"item-output",'
                           b'"output_index":0,"delta":"hidden","delta":"[]"}\n\n')
        candidates = [duplicate_event + terminal]
        for payload in ('{"value":"hidden","value":"alias"}', '[NaN]', '[Infinity]'):
            event = {"type": "response.output_text.delta", "item_id": "item-output", "output_index": 0,
                     "delta": payload}
            candidates.append(b"data: " + json.dumps(event).encode() + b"\n\n" + terminal)
        for wire in candidates:
            proof = harness.UpstreamResponseProof(max_bytes=4096)
            proof.record(wire, len(wire))
            with self.assertRaises(harness.CheckFailure):
                proof.finish()
            self.assertEqual(proof.buffered_bytes, 0)


if __name__ == "__main__":
    unittest.main()
