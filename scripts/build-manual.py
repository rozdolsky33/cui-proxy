#!/usr/bin/env python3
"""Build the embedded offline manual; run with the pinned docs virtualenv."""
import hashlib
import html
from urllib.parse import urlsplit, urlunsplit
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
EXCLUDED = {"superpowers", "adr", "spikes", "verification-results"}


def sources():
    paths = [ROOT / name for name in (
        "mkdocs.yml", "mkdocs-offline.yml", "requirements-docs.txt", "scripts/build-manual.py")]
    paths += [p for p in (ROOT / "docs").rglob("*")
              if p.is_file() and not EXCLUDED.intersection(p.relative_to(ROOT / "docs").parts)]
    paths += [p for p in (ROOT / "scripts/manual-assets").rglob("*") if p.is_file()]
    return {p.relative_to(ROOT).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(paths)}


def main():
    os.chdir(ROOT)
    with tempfile.TemporaryDirectory(prefix="cui-offline-manual-") as tmp:
        site = Path(tmp) / "site"
        subprocess.run([sys.executable, "-m", "mkdocs", "build", "--strict",
                        "-f", "mkdocs-offline.yml", "-d", str(site)], check=True)
        for name in ("sitemap.xml", "sitemap.xml.gz"):
            (site / name).unlink(missing_ok=True)  # dated discovery files are unnecessary offline
        for asset in (ROOT / "scripts/manual-assets").iterdir():
            (site / "assets" / asset.name).write_bytes(asset.read_bytes())
        for page in site.rglob("*.html"):
            text = page.read_text()
            # HTTP directory indexes are not resolved by file:// browsers.
            def file_link(match):
                url = urlsplit(html.unescape(match[1]))
                if not url.scheme and not url.netloc and url.path and (page.parent / url.path).is_dir():
                    path = url.path.rstrip("/") + "/index.html"
                    value = urlunsplit(("", "", path, url.query, url.fragment))
                    return 'href="' + html.escape(value, quote=True) + '"'
                return match[0]
            text = re.sub(r'href="([^"]+)"', file_link, text)
            # Material loads Mermaid from a CDN on demand. Keep its complete
            # readable diagram source offline; the prose explains the same flow.
            text = text.replace('<pre class="mermaid">',
                                '<p><em>Offline diagram source; see the walkthrough below.</em></p><pre>')
            if re.search(r'<(?:script|img)[^>]+src=["\'](?:https?:)?//', text):
                raise SystemExit(f"Automatic external resource in {page.name}")
            page.write_text(text)
        (site / "sources.json").write_text(json.dumps(sources(), sort_keys=True, indent=2) + "\n")
        dest = ROOT / "internal/manual/site.zip"
        dest.parent.mkdir(parents=True, exist_ok=True)
        with zipfile.ZipFile(dest, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
            for path in sorted(site.rglob("*")):
                if path.is_file():
                    info = zipfile.ZipInfo(path.relative_to(site).as_posix(), date_time=(1980, 1, 1, 0, 0, 0))
                    info.compress_type = zipfile.ZIP_DEFLATED
                    info.external_attr = 0o100644 << 16
                    archive.writestr(info, path.read_bytes(), compresslevel=9)
        print(f"Embedded manual: {dest.relative_to(ROOT)} ({dest.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
