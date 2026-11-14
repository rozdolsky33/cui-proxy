import copy
import unittest
from unittest.mock import patch
from test_soak_corpus import descriptors
from soak_corpus_attestation import attest_corpus


def manifest():
    return dict(version=2,count=100,images=descriptors(),pdfs=[
        dict(id="pdf-text",kind="text",pages=["one","two"],file="text.pdf",bytes=1,sha256=f"{800:064x}"),
        dict(id="pdf-scan",kind="scan",pages=["one","two"],file="scan.pdf",bytes=1,sha256=f"{801:064x}")])


def observed(root,row):
    return {k:row[k] for k in ("id","bytes","sha256")}


class CorpusAttestationTests(unittest.TestCase):
    def test_attestation_covers_all_files_and_oracle_metadata(self):
        original=manifest()
        with patch("soak_corpus_attestation.verify_image_file",side_effect=observed):
            _,proof=attest_corpus("unused",original)
            changed=copy.deepcopy(original);changed["images"][0]["expected"]="different oracle"
            _,other=attest_corpus("unused",changed)
        self.assertEqual(proof["verified_files"],802)
        self.assertEqual(proof["images"],800)
        self.assertEqual(proof["pdfs"],2)
        self.assertNotEqual(proof["manifest_sha256"],other["manifest_sha256"])
        self.assertEqual(proof["files_sha256"],other["files_sha256"])
        self.assertNotIn("expected",str(proof))

    def test_any_file_verification_failure_prevents_attestation(self):
        for failed in ["image-0-0","image-2-99","pdf-scan"]:
            def verify(root,row):
                if row["id"]==failed:raise ValueError("file_unavailable")
                return observed(root,row)
            with patch("soak_corpus_attestation.verify_image_file",side_effect=verify):
                with self.assertRaises(ValueError):attest_corpus("unused",manifest())

    def test_invalid_pdf_or_duplicate_identity_prevents_attestation(self):
        cases=[]
        for update in [dict(kind="other"),dict(pages=["one"]),dict(id="image-0-0"),dict(file="images/0-0.png")]:
            item=manifest();item["pdfs"][1].update(update);cases.append(item)
        with patch("soak_corpus_attestation.verify_image_file",side_effect=observed):
            for item in cases:
                with self.assertRaises(ValueError):attest_corpus("unused",item)
