"""Root campaign evidence linking, separate from acceptance evaluation."""
import copy
import hashlib
import json
import os
import re
from pathlib import Path
import stat
from soak_evidence import EvidenceWriter,read_sealed_evidence,_encode

PHASES=("proxy-fixed","proxy-churn","bypass-fixed","bypass-churn")


def _digest(path):
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        before=os.fstat(fd)
        if not stat.S_ISREG(before.st_mode):
            raise ValueError("campaign_artifact_type")
        remaining=before.st_size
        digest=hashlib.sha256()
        while remaining:
            part=os.read(fd,min(65536,remaining))
            if not part:
                raise ValueError("campaign_artifact_truncated")
            digest.update(part);remaining-=len(part)
        if os.read(fd,1):
            raise ValueError("campaign_artifact_grew")
        after=os.fstat(fd)
        if any(getattr(before,k)!=getattr(after,k) for k in ("st_dev","st_ino","st_size","st_mtime_ns","st_ctime_ns")):
            raise ValueError("campaign_artifact_changed")
        return digest.hexdigest()
    finally:
        os.close(fd)


class CampaignEvidence:
    """A completed root means linked artifacts, never performance acceptance."""
    performance_qualified=False

    def __init__(self,root,identity,settings):
        self.root=Path(root)
        self.identity=copy.deepcopy(identity)
        self.settings=copy.deepcopy(settings)
        self.links={}
        self.artifacts={}
        self.report_sizes = {}
        self.writer=None
        self.finished=False
        self.failed=False
        self.completion_requested=False

    def __enter__(self):
        self.root.mkdir(mode=0o700)
        self.writer=EvidenceWriter(self.root/"events.jsonl")
        self.writer.__enter__()
        try:
            self.writer({"type":"campaign_start","identity":self.identity,
                         "settings":self.settings,"performance_qualified":False})
            _sync_parent(self.root)
        except BaseException:
            try:
                self.writer.seal(self.root/"seal.json",completed=False)
            finally:
                self.writer.__exit__(None,None,None)
            raise
        return self

    def _active(self):
        if self.writer is None or self.finished:
            raise ValueError("campaign_not_active")

    def _phase_identity(self,name):
        directory=self.root/name
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError("campaign_phase_directory")
        events,seal=directory/"events.jsonl",directory/"seal.json"
        before={"events_sha256":_digest(events),"seal_sha256":_digest(seal)}
        report=read_sealed_evidence(events,seal)
        after={"events_sha256":_digest(events),"seal_sha256":_digest(seal)}
        if before!=after:
            raise ValueError("campaign_phase_changed")
        return dict(after,run_completed=report["run_completed"],event_count=len(report["events"]))

    def link_phase(self,name):
        self._active()
        if name not in PHASES or name in self.links:
            raise ValueError("campaign_phase_identity")
        if name!=PHASES[len(self.links)]:
            raise ValueError("campaign_phase_order")
        try:
            identity=self._phase_identity(name)
            self.writer({"type":"campaign_phase","phase":name,**identity})
            if identity["run_completed"] is not True:
                raise ValueError("campaign_phase_incomplete")
            self.links[name]=identity
        except (OSError,ValueError):
            self.failed=True
            raise

    def save_report(self,name,report):
        self._active()
        if not isinstance(name,str) or not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,100}\.json",name) or name in ("seal.json",):
            raise ValueError("campaign_artifact_name")
        if name in self.artifacts:
            raise ValueError("campaign_artifact_exists")
        encoded=_encode(report)+b"\n"
        digest=hashlib.sha256(encoded).hexdigest()
        try:
            fd=os.open(self.root/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        except FileExistsError as error:
            raise ValueError("campaign_artifact_exists") from error
        try:
            with os.fdopen(fd,"wb") as stream:
                stream.write(encoded)
                stream.flush()
                os.fsync(stream.fileno())
            parent=os.open(self.root,os.O_RDONLY|os.O_DIRECTORY)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
            self.writer({"type":"campaign_artifact","name":name,"sha256":digest,"bytes":len(encoded)})
            self.artifacts[name]=digest
            self.report_sizes[name] = len(encoded)
        except BaseException:
            self.failed=True
            raise
        return digest

    def load_report(self, name):
        """Read the exact registered bytes before using a report for replay."""
        self._active()
        if name not in self.artifacts or name not in self.report_sizes:
            raise ValueError("unregistered campaign report")
        expected_size = self.report_sizes[name]
        fd = os.open(self.root / name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            before = os.fstat(fd)
            if not stat.S_ISREG(before.st_mode) or before.st_size != expected_size:
                raise ValueError("campaign report size or type changed")
            chunks = []
            remaining = expected_size
            while remaining:
                chunk = os.read(fd, min(65536, remaining))
                if not chunk:
                    raise ValueError("campaign report truncated")
                chunks.append(chunk)
                remaining -= len(chunk)
            if os.read(fd, 1):
                raise ValueError("campaign report grew")
            after = os.fstat(fd)
            fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
            if any(getattr(before, field) != getattr(after, field) for field in fields):
                raise ValueError("campaign report changed during read")
            encoded = b"".join(chunks)
            if hashlib.sha256(encoded).hexdigest() != self.artifacts[name]:
                raise ValueError("campaign report identity changed")
            return json.loads(encoded)
        finally:
            os.close(fd)

    def finish(self):
        self._active()
        if self.failed or tuple(self.links)!=PHASES:
            raise ValueError("campaign_incomplete")
        for name,digest in self.artifacts.items():
            if _digest(self.root/name)!=digest:
                self.failed=True
                raise ValueError("campaign_artifact_changed")
        for name,identity in self.links.items():
            if self._phase_identity(name)!=identity:
                self.failed=True
                raise ValueError("campaign_phase_changed")
        self.completion_requested=True

    def __exit__(self,*args):
        completed=False
        try:
            if self.completion_requested and not args[0]:
                self.finish()  # Recheck child identities after caller cleanup.
                self.writer({"type":"campaign_finished","phase_count":len(self.links),
                             "performance_qualified":False})
                completed=True
        finally:
            try:
                self.writer.seal(self.root/"seal.json",completed=completed)
                self.finished=True
            finally:
                self.writer.__exit__(*args)


def _sync_parent(root):
    fd=os.open(Path(root).parent,os.O_RDONLY|os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
