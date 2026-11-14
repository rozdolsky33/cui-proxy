"""Campaign preflight: immutable descriptor snapshot and file identities."""
import copy
import hashlib
import json
import math
import os
import stat
from soak_corpus import CorpusSelection
from soak_corpus_files import verify_image_file


def _hash(value):
    return hashlib.sha256(json.dumps(value,sort_keys=True,separators=(",",":"),
                                     allow_nan=False).encode()).hexdigest()


def attest_corpus(root,manifest):
    """Verify every declared image/PDF before returning the selector and proof.

    OCR correctness and runtime qualification are separate requirements. The
    manifest hash includes the independent oracle and rendering provenance;
    the returned proof never includes those private values.
    """
    snapshot=copy.deepcopy(manifest)
    if not isinstance(snapshot,dict) or type(snapshot.get("version")) is not int or snapshot["version"]!=2:
        raise ValueError("corpus_manifest_version")
    if type(snapshot.get("count")) is not int or snapshot["count"]!=100:
        raise ValueError("corpus_manifest_count")
    images=snapshot.get("images")
    selector=CorpusSelection(images)
    pdfs=snapshot.get("pdfs")
    if not isinstance(pdfs,list) or len(pdfs)!=2:
        raise ValueError("corpus_pdf_count")
    for row in pdfs:
        if not isinstance(row,dict) or row.get("kind") not in ("text","scan"):
            raise ValueError("corpus_pdf_kind")
        pages=row.get("pages")
        if not isinstance(pages,list) or len(pages)!=2 or any(not isinstance(p,str) or not p for p in pages):
            raise ValueError("corpus_pdf_pages")
    if {row["kind"] for row in pdfs}!={"text","scan"}:
        raise ValueError("corpus_pdf_kind")
    rows=images+pdfs
    for field in ("id","file","sha256"):
        values=[row.get(field) for row in rows]
        if any(not isinstance(v,str) for v in values) or len(set(values))!=len(rows):
            raise ValueError("corpus_duplicate_descriptor")
    manifest_hash=_hash(snapshot)
    observed=[verify_image_file(root,row) for row in rows]
    proof=dict(manifest_sha256=manifest_hash,
               files_sha256=_hash(sorted(observed,key=lambda row:row["id"])),
               verified_files=len(observed),images=len(images),pdfs=len(pdfs))
    return selector,proof


def _pairs(items):
    result={}
    for key,value in items:
        if key in result:
            raise ValueError("corpus_manifest_duplicate")
        result[key]=value
    return result


def _number(value):
    result=float(value)
    if not math.isfinite(result):
        raise ValueError("corpus_manifest_nonfinite")
    return result


def load_manifest(root):
    """Read one bounded regular manifest without following its symlink."""
    directory=None
    file_fd=None
    try:
        directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        file_fd=os.open("manifest.json",os.O_RDONLY|os.O_NONBLOCK|os.O_NOFOLLOW,dir_fd=directory)
        before=os.fstat(file_fd)
        if not stat.S_ISREG(before.st_mode) or not 0 < before.st_size <= 4*1024*1024:
            raise ValueError("corpus_manifest_size_or_type")
        chunks=[]
        remaining=before.st_size
        while remaining:
            part=os.read(file_fd,min(65536,remaining))
            if not part:
                raise ValueError("corpus_manifest_truncated")
            chunks.append(part);remaining-=len(part)
        if os.read(file_fd,1):
            raise ValueError("corpus_manifest_grew")
        after=os.fstat(file_fd)
        if any(getattr(before,k)!=getattr(after,k) for k in ("st_dev","st_ino","st_size","st_mtime_ns","st_ctime_ns")):
            raise ValueError("corpus_manifest_changed")
        result=json.loads(b"".join(chunks).decode("utf-8"),object_pairs_hook=_pairs,
                          parse_float=_number,parse_constant=_number)
        if not isinstance(result,dict):
            raise ValueError("corpus_manifest_object")
        return result
    except (OSError,UnicodeError,json.JSONDecodeError,RecursionError) as error:
        raise ValueError("corpus_manifest_unreadable") from error
    finally:
        if file_fd is not None:
            os.close(file_fd)
        if directory is not None:
            os.close(directory)
