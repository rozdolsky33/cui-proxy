"""Private, bounded OCR gate for isolated cancellation diagnostics."""

import json
import os
from pathlib import Path
import re
import secrets
import stat
import sys
import tempfile


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate_gate_field')
        result[key] = value
    return result


def _read_marker(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size > 4096:
            raise ValueError('invalid_gate_marker')
        raw = stream.read(4097)
        after = os.fstat(stream.fileno())
    if len(raw) > 4096 or (before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
            after.st_size, after.st_mtime_ns, after.st_ctime_ns):
        raise ValueError('changed_gate_marker')
    return json.loads(raw, object_pairs_hook=_unique_object)


_WRAPPER = r'''import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import time

root = Path(@@ROOT@@)
delegate = @@DELEGATE@@

def run_real():
    os.execv(delegate, [delegate, *sys.argv[1:]])

def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate_gate_field')
        result[key] = value
    return result

if len(sys.argv) < 2 or sys.argv[1] != 'stdin':
    run_real()
descriptor = os.open(root / 'control.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
with os.fdopen(descriptor, 'rb') as stream:
    info = os.fstat(stream.fileno())
    if not stat.S_ISREG(info.st_mode) or info.st_size > 1024:
        raise ValueError('invalid_gate_control')
    control = json.loads(stream.read(1025), object_pairs_hook=unique)
if (not isinstance(control, dict) or set(control) != {'armed', 'generation'}
        or type(control['armed']) is not bool):
    raise ValueError('invalid_gate_control')
if not control['armed']:
    run_real()
generation = control['generation']
if not isinstance(generation, str) or not re.fullmatch(r'[0-9a-f]{32}', generation):
    raise ValueError('invalid_gate_generation')
directory = root / generation
if not stat.S_ISDIR(directory.lstat().st_mode):
    raise ValueError('invalid_gate_generation')
data = sys.stdin.buffer.read(80 * 1024 * 1024 + 1)
if len(data) > 80 * 1024 * 1024:
    raise ValueError('gate_input_limit')
digest = hashlib.sha256(data).hexdigest()
retained = tempfile.TemporaryFile()
retained.write(data)
retained.seek(0)
del data
os.dup2(retained.fileno(), 0, inheritable=True)
marker = dict(pid=os.getpid(), generation=generation,
              started_ns=time.monotonic_ns(), input_sha256=digest)
descriptor, temporary = tempfile.mkstemp(prefix='.marker-', dir=directory)
try:
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(marker, stream, sort_keys=True)
        stream.flush()
        os.fsync(stream.fileno())
    os.link(temporary, directory / (str(os.getpid()) + '.json'))
finally:
    os.unlink(temporary)
deadline = time.monotonic() + 30
while True:
    release = directory / 'release'
    try:
        info = release.lstat()
    except FileNotFoundError:
        info = None
    if info is not None:
        if not stat.S_ISREG(info.st_mode) or info.st_size:
            raise ValueError('invalid_gate_release')
        run_real()
    if time.monotonic() >= deadline:
        raise TimeoutError('gate_hold_deadline')
    time.sleep(0.01)
'''


class GatedOCR:
    """Keep gate files as evidence; the caller owns service/process cleanup."""

    def __init__(self, directory, delegate):
        self.directory = Path(directory).absolute()
        self.delegate = Path(delegate).resolve(strict=True)
        if not self.delegate.is_file() or not os.access(self.delegate, os.X_OK):
            raise ValueError('invalid_ocr_delegate')
        self.launcher = self.directory / 'bin' / 'tesseract'
        self.generation = None
        self.armed = False
        self._identity = None

    def _check_root(self):
        info = self.directory.lstat()
        if not stat.S_ISDIR(info.st_mode) or (info.st_dev, info.st_ino) != self._identity:
            raise ValueError('changed_gate_directory')

    def _control(self):
        self._check_root()
        descriptor, temporary = tempfile.mkstemp(prefix='.control-', dir=self.directory)
        try:
            with os.fdopen(descriptor, 'w') as stream:
                json.dump(dict(armed=self.armed,
                               generation=self.generation.name if self.generation else None), stream)
            os.replace(temporary, self.directory / 'control.json')
        finally:
            Path(temporary).unlink(missing_ok=True)

    def __enter__(self):
        interpreter = str(Path(sys.executable).resolve(strict=True))
        if any(character.isspace() for character in interpreter):
            raise ValueError('unsupported_gate_interpreter_path')
        self.directory.mkdir(mode=0o700)
        info = self.directory.lstat()
        self._identity = (info.st_dev, info.st_ino)
        self.launcher.parent.mkdir(mode=0o700)
        source = '#!' + interpreter + '\n' + _WRAPPER.replace(
            '@@ROOT@@', repr(str(self.directory))).replace('@@DELEGATE@@', repr(str(self.delegate)))
        descriptor = os.open(self.launcher, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o700)
        with os.fdopen(descriptor, 'w') as stream:
            stream.write(source)
        self._control()
        return self

    def arm(self):
        self._check_root()
        if self.armed:
            raise ValueError('gate_already_armed')
        self.generation = self.directory / secrets.token_hex(16)
        self.generation.mkdir(mode=0o700)
        self.armed = True
        self._control()
        return self.generation

    def release(self):
        self._check_root()
        if self.generation is not None:
            if not stat.S_ISDIR(self.generation.lstat().st_mode):
                raise ValueError('invalid_gate_generation')
            release = self.generation / 'release'
            try:
                descriptor = os.open(release, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            except FileExistsError:
                info = release.lstat()
                if not stat.S_ISREG(info.st_mode) or info.st_size:
                    raise ValueError('invalid_gate_release')
            else:
                os.close(descriptor)
        self.armed = False
        self._control()

    def snapshot(self):
        self._check_root()
        if self.generation is None:
            return []
        if not stat.S_ISDIR(self.generation.lstat().st_mode):
            raise ValueError('invalid_gate_generation')
        paths = list(self.generation.glob('*.json'))
        if len(paths) > 16:
            raise ValueError('gate_marker_limit')
        rows = []
        for path in paths:
            row = _read_marker(path)
            if (not isinstance(row, dict)
                    or set(row) != {'pid', 'generation', 'started_ns', 'input_sha256'}
                    or type(row['pid']) is not int or row['pid'] <= 0
                    or path.name != str(row['pid']) + '.json'
                    or row['generation'] != self.generation.name
                    or type(row['started_ns']) is not int or row['started_ns'] <= 0
                    or not isinstance(row['input_sha256'], str)
                    or not re.fullmatch(r'[0-9a-f]{64}', row['input_sha256'])):
                raise ValueError('invalid_gate_marker')
            rows.append(row)
        return sorted(rows, key=lambda row: (row['started_ns'], row['pid']))

    def __exit__(self, kind, error, traceback):
        self.release()
        return False
