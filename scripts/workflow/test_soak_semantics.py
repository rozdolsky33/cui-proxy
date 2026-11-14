import json
import pathlib
import tempfile
import unittest

import soak_semantics


class SoakSemanticsTests(unittest.TestCase):
    def call(self, scenario, arguments, call_id="case-1"):
        return {"type": "function_call", "name": "apply_edit" if scenario == "W02" else "plan_command",
                "call_id": call_id, "arguments": arguments}

    def test_backend_uses_received_values_only(self):
        call = soak_semantics.build_tool_call("W02", ["masked-one", "masked-two"], "case-1")
        self.assertEqual(json.loads(call["arguments"]),
                         {"path": "effect.txt", "old": "masked-one", "new": "masked-two"})
        self.assertEqual(call["call_id"], "case-1")

    def test_restored_arguments_apply_the_exact_file_effect(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "effect.txt"
            path.write_text("first value", encoding="utf-8")
            call = self.call("W02", json.dumps({"path": "effect.txt", "old": "first value", "new": "second value"}))
            result = soak_semantics.verify_tool_effect("W02", call, ["first value", "second value"], "case-1", directory)
            self.assertEqual(path.read_text(encoding="utf-8"), "second value")
            self.assertEqual(result, {"effect": "file_edit_verified"})

    def test_wrong_restoration_or_call_association_cannot_edit(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "effect.txt"
            for call in [self.call("W02", json.dumps({"path": "effect.txt", "old": "second value", "new": "first value"})),
                         self.call("W02", json.dumps({"path": "effect.txt", "old": "first value", "new": "second value"}), "wrong-call")]:
                path.write_text("first value", encoding="utf-8")
                with self.assertRaisesRegex(ValueError, "^soak_semantic_mismatch$"):
                    soak_semantics.verify_tool_effect("W02", call, ["first value", "second value"], "case-1", directory)
                self.assertEqual(path.read_text(encoding="utf-8"), "first value")

    def test_duplicate_keys_and_path_changes_are_rejected(self):
        for args in ['{"path":"effect.txt","old":"first","old":"first","new":"second"}',
                     json.dumps({"path": "../outside", "old": "first", "new": "second"})]:
            with self.assertRaisesRegex(ValueError, "^soak_semantic_mismatch$"):
                soak_semantics.verify_tool_effect("W02", self.call("W02", args), ["first", "second"], "case-1")

    def test_command_oracle_checks_parsed_tokens_without_execution(self):
        import shlex
        values = ["source with spaces", "target ' quoted ; text"]
        command = shlex.join(["inspect-resource", "--source", values[0], "--target", values[1]])
        call = self.call("W04", json.dumps({"command": command}))
        self.assertEqual(soak_semantics.verify_tool_effect("W04", call, values, "case-1"),
                         {"effect": "command_tokens_verified"})
        call["arguments"] = json.dumps({"command": command + " extra"})
        with self.assertRaisesRegex(ValueError, "^soak_semantic_mismatch$"):
            soak_semantics.verify_tool_effect("W04", call, values, "case-1")

    def test_symlink_effect_file_is_not_followed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            outside = root / "outside.txt"
            outside.write_text("first", encoding="utf-8")
            (root / "effect.txt").symlink_to(outside)
            call = self.call("W02", json.dumps({"path": "effect.txt", "old": "first", "new": "second"}))
            with self.assertRaisesRegex(ValueError, "^soak_semantic_mismatch$"):
                soak_semantics.verify_tool_effect("W02", call, ["first", "second"], "case-1", root)
            self.assertEqual(outside.read_text(encoding="utf-8"), "first")
