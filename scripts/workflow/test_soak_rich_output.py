import unittest
from performance import values_from_body


class RichOutputTests(unittest.TestCase):
    def test_rich_tool_outputs_keep_text_order_and_skip_image_bytes(self):
        for kind in ("function_call_output", "custom_tool_call_output"):
            body = {"input": [{"type": kind, "call_id": "one", "output": [
                {"type": "input_text", "text": "first"},
                {"type": "input_image", "image_url": "data:image/png;base64,AA=="},
                {"type": "input_text", "text": "second"}]}]}
            with self.subTest(kind=kind):
                self.assertEqual(values_from_body(body), ["first", "second"])

    def test_plain_custom_output_is_supported(self):
        body = {"input": [{"type": "custom_tool_call_output", "call_id": "one", "output": "first"}]}
        self.assertEqual(values_from_body(body), ["first"])
