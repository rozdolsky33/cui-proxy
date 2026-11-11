"""Tests catch overlapping cold/warm samples and permissive OCR oracles."""
import unittest
import os
import tempfile
from pathlib import Path
import extraction_fixtures as fixtures

def manifest_for_integrity():
    images=fixtures.recipes("warmup",1)+fixtures.recipes("measurement",1)
    pdfs=fixtures.pdf_recipes()
    for row in images+pdfs:
        row.update(file=row["id"]+"."+row.get("format","pdf"),bytes=1,sha256="unused")
    return dict(version=fixtures.VERSION,seed=1102026,count=1,images=images,pdfs=pdfs)

class ExtractionFixtureTests(unittest.TestCase):
    def test_cold_samples_have_distinct_printed_indices_and_disjoint_warmup(self):
        warm = fixtures.recipes("warmup", 100)
        cold = fixtures.recipes("measurement", 100)
        self.assertEqual(len(cold), 400)
        self.assertEqual(len({r["id"] for r in cold}), 400)
        self.assertFalse({r["index"] for r in warm} & {r["index"] for r in cold})
        for template in range(4):
            rows = [r for r in cold if r["template"] == template]
            self.assertEqual(len({r["expected"] for r in rows}), 100)
            self.assertTrue(all(f'{r["index"]:06d}' in r["expected"] for r in rows))
        self.assertEqual({(r["format"],r["width"],r["height"]) for r in cold},
                         {("png",1024,768),("jpeg",1024,768),("png",1920,1080),("jpeg",1920,1080)})

    def test_invalid_sample_ranges_are_rejected(self):
        for phase,count in [("unknown",1),("warmup",0),("measurement",101),("measurement",True)]:
            with self.assertRaises(ValueError): fixtures.recipes(phase,count)

    def test_oracle_tolerates_layout_but_rejects_wrong_index_missing_and_extra_words(self):
        expected = "Sample 100001\nThe river flows past the garden."
        self.assertTrue(fixtures.matches(expected, "Sample   100001\n\nThe river flows past the garden.\f"))
        for actual in [expected.replace("100001","100002"), expected.replace("river ",""),expected+" extra",expected.lower()]:
            self.assertFalse(fixtures.matches(expected,actual))

    def test_pdf_recipe_is_two_pages_with_independent_expectations(self):
        rows = fixtures.pdf_recipes()
        self.assertEqual([r["kind"] for r in rows],["text","scan"])
        for row in rows:
            self.assertEqual(len(row["pages"]),2)
            self.assertNotEqual(row["pages"][0],row["pages"][1])
            self.assertIn("Page 1",row["pages"][0])
            self.assertIn("Page 2",row["pages"][1])

    @unittest.skipUnless(os.environ.get("CUI_REAL_EXTRACTION_TEST") == "1", "explicit real extraction test")
    def test_real_renderer_and_extractors_match_independent_text(self):
        with tempfile.TemporaryDirectory() as root:
            result = fixtures.generate(Path(root) / "fixtures", count=2)
            self.assertEqual(result["font_name"],"Menlo-Regular")
            self.assertEqual(set(result["tool_versions"]),{"swift","tesseract","pdftotext","pdftoppm"})
            self.assertTrue(all(result["tool_versions"].values()))
            self.assertRegex(result["traineddata_sha256"],r"^[a-f0-9]{64}$")
            self.assertEqual(len(result["images"]),16)
            self.assertEqual(len(result["pdfs"]),2)
            self.assertEqual(len({r["sha256"] for r in result["images"]}),16)
            self.assertTrue(all(0 < r["bytes"] <= 2*1024*1024 for r in result["images"]+result["pdfs"]))
            self.assertEqual(fixtures.validate(Path(root) / "fixtures"),18)
            self.assertEqual((Path(root) / "fixtures" / "manifest.json").stat().st_mode & 0o777,0o600)

    def test_changed_fixture_is_rejected_before_extractor_runs(self):
        import json
        import hashlib
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            image = root / "sample.png"
            image.write_bytes(b"changed")
            manifest=manifest_for_integrity()
            manifest["images"][0].update(file="sample.png",sha256=hashlib.sha256(b"original").hexdigest(),bytes=8)
            (root / "manifest.json").write_text(json.dumps(manifest))
            with self.assertRaisesRegex(RuntimeError,"fixture_integrity_invalid"):
                fixtures.validate(root)

    def test_manifest_cannot_read_outside_fixture_directory(self):
        import json
        with tempfile.TemporaryDirectory() as root:
            root=Path(root)
            directory=root / "fixtures";directory.mkdir()
            manifest=manifest_for_integrity()
            manifest["images"][0]["file"]="../outside.png"
            (directory / "manifest.json").write_text(json.dumps(manifest))
            with self.assertRaisesRegex(RuntimeError,"fixture_path_invalid"):
                fixtures.validate(directory)

    def test_manifest_coverage_rejects_empty_missing_duplicate_or_changed_recipe(self):
        import copy
        rows=fixtures.recipes("warmup",2)+fixtures.recipes("measurement",2)
        manifest=dict(version=fixtures.VERSION,seed=1102026,count=2,images=rows,pdfs=fixtures.pdf_recipes())
        fixtures.validate_coverage(manifest)
        for change in ["empty","missing","duplicate","expectation","version","phase"]:
            bad=copy.deepcopy(manifest)
            if change=="empty": bad["images"]=[]
            if change=="missing": bad["pdfs"].pop()
            if change=="duplicate": bad["images"][1]=bad["images"][0]
            if change=="expectation": bad["images"][0]["expected"]="different"
            if change=="version": bad["version"]=-1
            if change=="phase": bad["images"][0]["index"]=100000
            with self.subTest(change=change),self.assertRaisesRegex(ValueError,"fixture_coverage_invalid"):
                fixtures.validate_coverage(bad)
