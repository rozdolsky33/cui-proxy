"""Bounded archive checks for retained release bytes; never builds a binary."""
import gzip
import hashlib
import io
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import tarfile

from soak_campaign_reader import _capture, read_report_bytes


STOCK_FILES = frozenset(("cui-proxy", "LICENSE", "README.md", "examples/config.yaml", "examples/hosts.txt"))
STOCK_MEMBERS = STOCK_FILES | {"examples"}
MAX_ARCHIVE_BYTES = 64 * 1024 * 1024
MAX_MEMBER_BYTES = 64 * 1024 * 1024
MAX_TOTAL_BYTES = 96 * 1024 * 1024
_HEX = re.compile(r"[0-9a-f]{64}\Z")


def require(condition, code):
    if not condition:
        raise ValueError("release_" + code)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def capture_file(path, limit=MAX_MEMBER_BYTES):
    path = Path(path).absolute()
    require(all(not part.is_symlink() for part in (path, *path.parents)), "file_symlink")
    try:
        info = path.stat()
    except OSError:
        raise ValueError("release_file_unavailable") from None
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= limit, "file_type_or_size")
    return _capture(path, expected_size=info.st_size)


def file_sha256(path):
    return sha256(capture_file(path))


def _members(data, *, strict):
    found = {}
    total = 0
    try:
        with gzip.GzipFile(fileobj=io.BytesIO(data), mode="rb") as compressed:
            expanded = compressed.read(MAX_TOTAL_BYTES + 65537)
        require(len(expanded) <= MAX_TOTAL_BYTES + 65536, "archive_expanded_size")
        with tarfile.open(fileobj=io.BytesIO(expanded), mode="r:") as source:
            last_member_end = 0
            for member in source:
                last_member_end = member.offset_data + ((member.size + 511) // 512) * 512
                path = PurePosixPath(member.name)
                require(not path.is_absolute() and path.parts and ".." not in path.parts
                        and "\\" not in member.name, "archive_path")
                name = str(path)
                require(name not in found and len(found) < 64, "archive_duplicate_or_count")
                require(member.isfile() or member.isdir(), "archive_member_type")
                require(0 <= member.size <= MAX_MEMBER_BYTES, "archive_member_size")
                total += member.size
                require(total <= MAX_TOTAL_BYTES, "archive_total_size")
                if strict:
                    require(name in STOCK_MEMBERS and (member.isdir() == (name == "examples")), "archive_member_layout")
                if member.isdir():
                    found[name] = None
                    continue
                stream = source.extractfile(member)
                require(stream is not None, "archive_member_unavailable")
                with stream:
                    content = stream.read(member.size + 1)
                require(len(content) == member.size, "archive_member_truncated")
                if name == "cui-proxy":
                    require(member.mode & 0o111, "archive_binary_not_executable")
                found[name] = content
            tail = expanded[last_member_end:]
            require(len(tail) >= 1024 and not any(tail), "archive_trailing_content")
    except (tarfile.TarError, OSError, EOFError):
        raise ValueError("release_archive_invalid") from None
    require("cui-proxy" in found and found["cui-proxy"] is not None, "archive_binary_missing")
    if strict:
        require(set(found) == STOCK_MEMBERS, "archive_member_layout")
    return found


def _verify(archive, expected_archive_sha256, expected_binary_sha256, source_root, strict):
    require(type(expected_archive_sha256) is str and _HEX.fullmatch(expected_archive_sha256)
            and type(expected_binary_sha256) is str and _HEX.fullmatch(expected_binary_sha256), "archive_expected_hash")
    data = capture_file(archive, MAX_ARCHIVE_BYTES)
    require(sha256(data) == expected_archive_sha256, "archive_digest")
    members = _members(data, strict=strict)
    require(sha256(members["cui-proxy"]) == expected_binary_sha256, "archive_binary_digest")
    if source_root is not None:
        for name in STOCK_FILES - {"cui-proxy"}:
            require(members.get(name) == capture_file(Path(source_root) / name, 2 * 1024 * 1024),
                    "archive_source_member_digest")
    metadata = {"archive_sha256": expected_archive_sha256, "binary_sha256": expected_binary_sha256,
                "archive_bytes": len(data), "stock_member_layout_verified": strict,
                "stock_builder_executed_by_probe": False,
                "member_sha256": {name: sha256(value) for name, value in members.items() if value is not None}}
    return members, metadata


def verify_archive(archive, expected_archive_sha256, expected_binary_sha256, *, source_root=None, strict=True):
    return _verify(archive, expected_archive_sha256, expected_binary_sha256, source_root, strict)[1]


def extract_archive(archive, destination, expected_archive_sha256, expected_binary_sha256, *, source_root=None):
    members, _metadata = _verify(archive, expected_archive_sha256, expected_binary_sha256, source_root, True)
    destination = Path(destination)
    created = False
    try:
        destination.mkdir(mode=0o700)
        created = True
        (destination / "examples").mkdir(mode=0o700)
        for name in sorted(STOCK_FILES):
            target = destination / name
            with target.open("xb") as output:
                output.write(members[name])
            target.chmod(0o755 if name == "cui-proxy" else 0o600)
        require(file_sha256(destination / "cui-proxy") == expected_binary_sha256, "extracted_binary_digest")
        return destination / "cui-proxy"
    except BaseException:
        if created:
            shutil.rmtree(destination)
        raise


def create_verification_archive(binary, source_root, output, expected_binary_sha256):
    """Explicit fallback: preserve bytes and layout, never claim package.sh ran."""
    contents = {name: capture_file(Path(source_root) / name, 2 * 1024 * 1024)
                for name in STOCK_FILES - {"cui-proxy"}}
    contents["cui-proxy"] = capture_file(binary)
    require(sha256(contents["cui-proxy"]) == expected_binary_sha256, "verification_binary_digest")
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as target:
                directory = tarfile.TarInfo("examples")
                directory.type = tarfile.DIRTYPE
                directory.mode = 0o755
                target.addfile(directory)
                for name in sorted(contents):
                    info = tarfile.TarInfo(name)
                    info.mode = 0o755 if name == "cui-proxy" else 0o644
                    info.size = len(contents[name])
                    target.addfile(info, io.BytesIO(contents[name]))
        result = verify_archive(output, file_sha256(output), expected_binary_sha256, source_root=source_root)
        result["packaging_method"] = "exact_byte_verification_archive"
        return result
    except BaseException:
        Path(output).unlink(missing_ok=True)
        raise


def _descriptor(root, descriptor, limit):
    require(type(descriptor) is dict and set(descriptor) == {"path", "sha256", "bytes"}, "provenance_descriptor")
    name = descriptor["path"]
    require(type(name) is str and 0 < len(name) <= 256, "provenance_path")
    path = PurePosixPath(name)
    require(not path.is_absolute() and path.parts and ".." not in path.parts and "\\" not in name, "provenance_path")
    data = capture_file(root / name, limit)
    require(type(descriptor["bytes"]) is int and len(data) == descriptor["bytes"]
            and type(descriptor["sha256"]) is str and sha256(data) == descriptor["sha256"], "provenance_file_digest")
    return root / name, data


def verify_baseline_provenance(binary, provenance):
    raw = capture_file(provenance, 64 * 1024)
    record = read_report_bytes(raw)
    fields = {"schema_version", "kind", "version", "release_tag", "source_commit", "binary_sha256", "asset", "checksums"}
    require(type(record) is dict and set(record) == fields and type(record["schema_version"]) is int
            and record["schema_version"] == 1 and record["kind"] == "operator_supplied_release_asset", "baseline_provenance")
    require(type(record["version"]) is str and re.fullmatch(r"\d+\.\d+\.\d+", record["version"])
            and record["release_tag"] == "v" + record["version"], "baseline_version")
    require(record["source_commit"] is None or (type(record["source_commit"]) is str
            and re.fullmatch(r"[0-9a-f]{40}", record["source_commit"])), "baseline_source")
    binary_hash = file_sha256(binary)
    require(binary_hash == record["binary_sha256"], "baseline_binary_digest")
    root = Path(provenance).absolute().parent
    asset, _asset_data = _descriptor(root, record["asset"], MAX_ARCHIVE_BYTES)
    _checksums_path, checksum_data = _descriptor(root, record["checksums"], 64 * 1024)
    try:
        rows = [line.split() for line in checksum_data.decode("ascii").splitlines() if line.strip()]
    except UnicodeError:
        raise ValueError("release_baseline_checksums") from None
    require(rows and all(len(row) == 2 and _HEX.fullmatch(row[0]) for row in rows), "baseline_checksums")
    names = [PurePosixPath(row[1].lstrip("*")) for row in rows]
    require(all(not name.is_absolute() and ".." not in name.parts for name in names), "baseline_checksums")
    matched = [row[0] for row, name in zip(rows, names) if name.name == asset.name]
    require(matched == [record["asset"]["sha256"]], "baseline_asset_checksum")
    verify_archive(asset, record["asset"]["sha256"], binary_hash, strict=False)
    return {"version": record["version"], "release_tag": record["release_tag"],
            "source_commit": record["source_commit"], "binary_sha256": binary_hash,
            "provenance_sha256": sha256(raw), "asset_sha256": record["asset"]["sha256"],
            "checksums_sha256": record["checksums"]["sha256"], "asset_verified": True,
            "attribution": "operator_supplied_release_asset"}
