"""Versioned real-extraction recipes. Executed manifests are private artifacts."""
VERSION = 2
SEED = 1102026
REFERENCE = "The river flows past the garden.\nClear morning light fills the room."
TEMPLATES = (("png",1024,768),("jpeg",1024,768),("png",1920,1080),("jpeg",1920,1080))


def recipes(phase, count=100):
    if phase not in ("warmup", "measurement") or type(count) is not int or not 1 <= count <= 100:
        raise ValueError("invalid_sample_range")
    offset = 0 if phase == "warmup" else 100000
    result = []
    for template, (fmt, width, height) in enumerate(TEMPLATES):
        for serial in range(count):
            index = offset + serial
            result.append(dict(id=f"image-{template}-{index:06d}", template=template,
                               index=index, format=fmt, width=width, height=height,
                               expected=f"Sample {index:06d}\n{REFERENCE}"))
    return result


def pdf_recipes():
    return [dict(id=f"pdf-{kind}", kind=kind,
                 pages=[f"Page {page}\n{REFERENCE}" for page in (1,2)])
            for kind in ("text", "scan")]



def validate_coverage(manifest):
    try:
        if type(manifest["version"]) is not int or manifest["version"] != VERSION or manifest["seed"] != SEED:
            raise ValueError
        count = manifest["count"]
        expected_images = recipes("warmup",count)+recipes("measurement",count)
        for key,expected in (("images",expected_images),("pdfs",pdf_recipes())):
            actual = manifest[key]
            if len(actual) != len(expected):
                raise ValueError
            lookup = {row["id"]:row for row in actual}
            if len(lookup) != len(expected):
                raise ValueError
            for recipe in expected:
                row=lookup[recipe["id"]]
                if any(type(row[k]) is not type(v) or row[k] != v for k,v in recipe.items()):
                    raise ValueError
    except (KeyError,TypeError,ValueError):
        raise ValueError("fixture_coverage_invalid") from None


def matches(expected, extracted):
    return expected.split() == extracted.split()


# Tools are invoked with captured output; neither tool errors nor extracted text
# are printed. The caller receives fixed diagnostic codes only.
def _tool(args, timeout=120, combined=False):
    import subprocess
    try:
        run = subprocess.run(args, capture_output=True, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise RuntimeError("extraction_dependency_failure") from error
    if run.returncode:
        raise RuntimeError("extraction_dependency_failure")
    return run.stdout + run.stderr if combined else run.stdout


def provenance():
    import re
    import hashlib
    from pathlib import Path
    versions = {}
    for tool,args,pattern in (
        ("swift",["swift","--version"],r"Swift version ([0-9]+\.[0-9]+(?:\.[0-9]+)?)"),
        ("tesseract",["tesseract","--version"],r"tesseract ([0-9]+\.[0-9]+\.[0-9]+)"),
        ("pdftotext",["pdftotext","-v"],r"version ([0-9]+\.[0-9]+\.[0-9]+)"),
        ("pdftoppm",["pdftoppm","-v"],r"version ([0-9]+\.[0-9]+\.[0-9]+)")):
        match = re.search(pattern,_tool(args,combined=True).decode())
        if not match:
            raise RuntimeError("tool_provenance_unavailable")
        versions[tool] = match.group(1)
    match = re.search(r'"([^"\n]+)"',_tool(["tesseract","--list-langs"],combined=True).decode())
    if not match:
        raise RuntimeError("traineddata_provenance_unavailable")
    traineddata = Path(match.group(1)) / "eng.traineddata"
    return dict(tool_versions=versions,traineddata_sha256=hashlib.sha256(traineddata.read_bytes()).hexdigest())


def generate(directory, count=100):
    import hashlib
    import json
    import os
    import platform
    from pathlib import Path
    if platform.system() != "Darwin":
        raise RuntimeError("reference_renderer_requires_macos")
    tools = provenance()
    images = recipes("warmup", count) + recipes("measurement", count)
    pdfs = pdf_recipes()
    directory = Path(directory)
    directory.mkdir(mode=0o700, parents=False, exist_ok=False)
    def private(name, value):
        with os.fdopen(os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as stream:
            json.dump(value,stream,indent=2)
    private("recipes.json",dict(images=images,pdfs=pdfs))
    _tool(["swift",str(Path(__file__).with_name("render_extraction.swift")),str(directory)],timeout=300)
    for row in images + pdfs:
        extension = row.get("format", "pdf")
        path = directory / (row["id"] + "." + extension)
        path.chmod(0o600)
        data = path.read_bytes()
        if not 0 < len(data) <= 2 * 1024 * 1024:
            raise RuntimeError("fixture_size_invalid")
        row.update(file=path.name, bytes=len(data), sha256=hashlib.sha256(data).hexdigest())
    font = directory / "font.bin"
    manifest = dict(version=VERSION, seed=SEED, count=count, renderer="CoreText/Menlo-Regular/28",
                    os=platform.mac_ver(),font_name=json.loads((directory / "font.json").read_text())["postscript_name"],font_sha256=hashlib.sha256(font.read_bytes()).hexdigest(),
                    images=images,pdfs=pdfs)
    manifest.update(tools)
    font.unlink()
    (directory / "font.json").unlink()
    private("manifest.json",manifest)
    return manifest


def validate(directory):
    import json
    import tempfile
    from pathlib import Path
    directory = Path(directory)
    manifest = json.loads((directory / "manifest.json").read_text())
    validate_coverage(manifest)
    import hashlib
    root = directory.resolve()
    for row in manifest["images"] + manifest["pdfs"]:
        name = row["file"]
        path = directory / name
        if Path(name).name != name or path.resolve().parent != root or path.is_symlink():
            raise RuntimeError("fixture_path_invalid")
        data = path.read_bytes()
        if len(data) != row["bytes"] or hashlib.sha256(data).hexdigest() != row["sha256"]:
            raise RuntimeError("fixture_integrity_invalid")
    observed = provenance()
    if manifest.get("font_name") != "Menlo-Regular" or any(manifest.get(k) != v for k,v in observed.items()):
        raise RuntimeError("fixture_provenance_changed")
    checked = 0
    for row in manifest["images"]:
        actual = _tool(["tesseract",str(directory / row["file"]),"stdout","-l","eng","--psm","6"]).decode()
        if not matches(row["expected"],actual):
            raise RuntimeError("image_text_mismatch")
        checked += 1
    for row in manifest["pdfs"]:
        path = directory / row["file"]
        if row["kind"] == "text":
            actual = _tool(["pdftotext","-layout",str(path),"-"]).decode()
        else:
            with tempfile.TemporaryDirectory() as root:
                prefix = str(Path(root) / "page")
                _tool(["pdftoppm","-r","150","-png",str(path),prefix])
                pages = sorted(Path(root).glob("page-*.png"))
                if len(pages) != 2:
                    raise RuntimeError("pdf_page_count_invalid")
                actual = "\n".join(_tool(["tesseract",str(page),"stdout","-l","eng","--psm","6"]).decode() for page in pages)
        if not matches("\n".join(row["pages"]),actual):
            raise RuntimeError("pdf_text_mismatch")
        checked += 1
    return checked


def main():
    import argparse
    from pathlib import Path
    parser = argparse.ArgumentParser(description="Generate private reference OCR/PDF fixtures on macOS")
    parser.add_argument("directory",type=Path)
    parser.add_argument("--count",type=int,default=100)
    args = parser.parse_args()
    try:
        generate(args.directory,args.count)
        count = validate(args.directory)
    except (OSError, ValueError, RuntimeError):
        print("fixture generation or validation failed; performance_qualified=false")
        return 1
    print(f"validated_fixtures={count}; performance_qualified=false")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
