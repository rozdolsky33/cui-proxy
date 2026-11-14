"""Bounded, no-follow verification of private fixture files."""
import hashlib
import os
import re
import stat
from pathlib import PurePosixPath


def verify_image_file(root, descriptor):
    """Verify one declared file without following fixture-path symlinks.

    This attests bytes observed now, not future filesystem immutability. Campaign
    preflight and final identity checks must bracket use of the private corpus.
    """
    filename=descriptor.get("file")
    size=descriptor.get("bytes")
    digest=descriptor.get("sha256")
    identity=descriptor.get("id")
    if type(size) is not int or not 0 < size <= 2*1024*1024:
        raise ValueError("corpus_file_size")
    if not isinstance(digest,str) or not re.fullmatch(r"[0-9a-f]{64}",digest):
        raise ValueError("corpus_file_digest")
    if not isinstance(identity,str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}",identity):
        raise ValueError("corpus_file_identity")
    if not isinstance(filename,str) or not filename or "\\" in filename or "\x00" in filename:
        raise ValueError("corpus_file_path")
    parts=filename.split("/")
    if PurePosixPath(filename).is_absolute() or any(p in ("",".","..") for p in parts):
        raise ValueError("corpus_file_path")
    directory=None
    file_fd=None
    try:
        directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        for part in parts[:-1]:
            child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=directory)
            os.close(directory)
            directory=child
        file_fd=os.open(parts[-1],os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=directory)
        before=os.fstat(file_fd)
        if not stat.S_ISREG(before.st_mode) or before.st_size != size:
            raise ValueError("corpus_file_type_or_size")
        observed=hashlib.sha256()
        remaining=size
        while remaining:
            chunk=os.read(file_fd,min(65536,remaining))
            if not chunk:
                raise ValueError("corpus_file_truncated")
            observed.update(chunk)
            remaining-=len(chunk)
        if os.read(file_fd,1):
            raise ValueError("corpus_file_grew")
        after=os.fstat(file_fd)
        fields=("st_dev","st_ino","st_size","st_mtime_ns","st_ctime_ns")
        if any(getattr(before,k)!=getattr(after,k) for k in fields):
            raise ValueError("corpus_file_changed")
        if observed.hexdigest()!=digest:
            raise ValueError("corpus_file_digest_mismatch")
        return {"id":identity,"bytes":size,"sha256":digest}
    except OSError as error:
        raise ValueError("corpus_file_unavailable") from error
    finally:
        if file_fd is not None:
            os.close(file_fd)
        if directory is not None:
            os.close(directory)
