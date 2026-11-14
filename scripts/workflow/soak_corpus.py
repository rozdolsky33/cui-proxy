"""Metadata-only selection; file containment and hash verification happen in preflight."""
import copy
import re
from pathlib import PurePosixPath
from soak_schedule import plan_request


class CorpusSelection:
    """Select from four templates with 100 warmup and 100 measured images each."""
    def __init__(self, images):
        if not isinstance(images, (list, tuple)) or len(images) != 800:
            raise ValueError("corpus_count")
        self._groups = {}
        ids, hashes, files = set(), set(), set()
        for original in images:
            if not isinstance(original, dict):
                raise ValueError("corpus_descriptor")
            row = copy.deepcopy(original)
            for field in ("template", "index", "width", "height", "bytes"):
                if type(row.get(field)) is not int or row[field] < 0:
                    raise ValueError("corpus_integer")
            if row["template"] not in range(4) or not (0 <= row["index"] < 100 or 100000 <= row["index"] < 100100):
                raise ValueError("corpus_partition")
            if row.get("format") not in ("png", "jpeg") or row["width"] <= 0 or row["height"] <= 0 or not 0 < row["bytes"] <= 2 * 1024 * 1024:
                raise ValueError("corpus_image")
            if not isinstance(row.get("expected"), str) or not row["expected"]:
                raise ValueError("corpus_expected")
            identity, digest, filename = row.get("id"), row.get("sha256"), row.get("file")
            if not isinstance(identity, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", identity):
                raise ValueError("corpus_identity")
            if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
                raise ValueError("corpus_digest")
            if not isinstance(filename, str) or not filename or "\\" in filename or "\x00" in filename:
                raise ValueError("corpus_path")
            path = PurePosixPath(filename)
            if path.is_absolute() or any(part in ("", ".", "..") for part in filename.split("/")):
                raise ValueError("corpus_path")
            if identity in ids or digest in hashes or filename in files:
                raise ValueError("corpus_duplicate")
            ids.add(identity)
            hashes.add(digest)
            files.add(filename)
            group = self._groups.setdefault(row["template"], {})
            if row["index"] in group:
                raise ValueError("corpus_duplicate_index")
            group[row["index"]] = row
        expected_indices = set(range(100)) | set(range(100000,100100))
        if set(self._groups) != set(range(4)) or any(set(group) != expected_indices for group in self._groups.values()):
            raise ValueError("corpus_partition")
        shapes = set()
        for group in self._groups.values():
            variants = {(r["format"], r["width"], r["height"]) for r in group.values()}
            if len(variants) != 1:
                raise ValueError("corpus_template")
            shapes.update(variants)
        if shapes != {(fmt,w,h) for fmt in ("png","jpeg") for w,h in ((1024,768),(1920,1080))}:
            raise ValueError("corpus_templates")

    def select(self, plan, *, warmup=False):
        if not isinstance(plan, dict) or type(warmup) is not bool:
            raise ValueError("corpus_schedule")
        try:
            phase, client, serial = plan["identity"]
            canonical = plan_request(client, serial, phase)
        except (KeyError, TypeError, ValueError) as error:
            raise ValueError("corpus_schedule") from error
        if canonical["category"] != "image" or any(plan.get(k) != v for k,v in canonical.items() if k != "identity"):
            raise ValueError("corpus_schedule")
        ordinal = ((serial // 10) * 2 + (serial % 10 - 7)) * 4 + client
        first = ordinal % 4
        index = (ordinal // 4 + (50 if phase == "churn" else 0)) % 100
        if warmup:
            index += 100000
        return [copy.deepcopy(self._groups[group][index]) for group in (first,(first+1)%4)]

    def evidence(self, plan, *, warmup=False):
        """Value-free ordered identities suitable for durable schedule evidence."""
        return [{"id": row["id"], "sha256": row["sha256"]}
                for row in self.select(plan, warmup=warmup)]

    def resolve(self, plan, evidence, *, warmup=False):
        """Reject changed or mismatched fixture metadata before replay sends."""
        selected = self.select(plan, warmup=warmup)
        expected = [{"id": row["id"], "sha256": row["sha256"]}
                    for row in selected]
        if type(evidence) is not list or evidence != expected:
            raise ValueError("corpus_replay_selection")
        return selected
