#!/usr/bin/env python3
"""Prepare pinned build assets; document processing itself never uses the network."""
import hashlib
import json
from pathlib import Path
import platform
import shutil
import ssl
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
LOCK = json.loads((ROOT / "scripts/native-assets.json").read_text())
DEPS = ROOT / ".deps"
ASSETS = ROOT / "bin/assets"

def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()

def _certificate_error(error):
    reason = getattr(error, "reason", error)
    return (
        isinstance(reason, (ssl.CertificateError, ssl.SSLCertVerificationError))
        or "CERTIFICATE_VERIFY_FAILED" in str(reason)
    )

def _download_with_curl(url, target):
    curl = shutil.which("curl")
    if not curl:
        return False
    subprocess.run(
        [
            curl,
            "--fail",
            "--location",
            "--retry",
            "2",
            "--retry-delay",
            "1",
            "--connect-timeout",
            "20",
            "--max-time",
            "180",
            "--silent",
            "--show-error",
            "--output",
            str(target),
            "--",
            url,
        ],
        check=True,
    )
    return True

def download(url, target, expected=None):
    if target.exists() and (expected is None or digest(target) == expected):
        return target
    target.parent.mkdir(parents=True, exist_ok=True)
    temp = target.with_suffix(target.suffix + ".partial")
    for attempt in range(3):
        try:
            request = urllib.request.Request(url, headers={"User-Agent": "chiedi-build"})
            try:
                with urllib.request.urlopen(request, timeout=180) as response, temp.open("wb") as output:
                    shutil.copyfileobj(response, output)
            except (urllib.error.URLError, ssl.SSLError) as error:
                # Some Python installations do not know the host OS certificate
                # store (notably Homebrew/embedded Python on macOS). Use curl's
                # native trust store in that case; never disable TLS verification.
                if not _certificate_error(error):
                    raise
                if not _download_with_curl(url, temp):
                    raise RuntimeError(
                        "Python cannot verify HTTPS certificates and curl is not installed; "
                        "install curl or configure Python's CA store."
                    ) from error
            if expected and digest(temp) != expected:
                raise RuntimeError("Checksum mismatch: " + url)
            temp.replace(target)
            return target
        except Exception:
            temp.unlink(missing_ok=True)
            if attempt == 2:
                raise
            time.sleep(2)

def copy_member(archive, suffix, destination):
    # Extract just the required regular file; never follow archive symlinks.
    members = [m for m in archive.getmembers() if m.isfile() and m.name.endswith(suffix)]
    if len(members) != 1:
        raise RuntimeError("Expected one archive member: " + suffix)
    destination.parent.mkdir(parents=True, exist_ok=True)
    with archive.extractfile(members[0]) as source, destination.open("wb") as output:
        shutil.copyfileobj(source, output)

def main():
    target = subprocess.check_output(["go", "env", "GOOS", "GOARCH"], text=True).split()
    host_arch = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    host_os = platform.system().lower()
    key = "-".join(target)
    if key not in LOCK["platforms"] or target != [host_os, host_arch]:
        raise RuntimeError("Build natively on Linux/macOS amd64 or arm64; cross compilation is not configured.")
    native = LOCK["platforms"][key]
    ASSETS.mkdir(parents=True, exist_ok=True)
    notices = ASSETS / "licenses"
    notices.mkdir(exist_ok=True)
    for name, config in native.items():
        archive_path = download(config["url"], DEPS / "downloads" / config["url"].rsplit("/", 1)[1], config["sha256"])
        with tarfile.open(archive_path) as archive:
            if name == "ort":
                for header in ["onnxruntime_c_api.h", "onnxruntime_ep_c_api.h"]:
                    copy_member(archive, "/include/" + header, DEPS / "include" / header)
                suffix = "/lib/libonnxruntime." + LOCK["onnxruntime"] + ".dylib" if host_os == "darwin" else "/lib/libonnxruntime.so." + LOCK["onnxruntime"]
                library = "libonnxruntime.dylib" if host_os == "darwin" else "libonnxruntime.so"
                copy_member(archive, suffix, ASSETS / library)
                for filename in ["LICENSE", "ThirdPartyNotices.txt"]:
                    copy_member(archive, "/" + filename, notices / ("onnxruntime-" + filename))
            else:
                copy_member(archive, "libtokenizers.a", DEPS / "lib/libtokenizers.a")
    base = "https://huggingface.co/" + LOCK["model"] + "/resolve/" + LOCK["revision"] + "/"
    # Immutable revision, never 'main' or a first-run runtime download.
    model = download(base + "onnx/model_qint8_avx512_vnni.onnx", DEPS / "downloads/e5-ccc66d3-int8.onnx", LOCK["model_sha256"])
    shutil.copy2(model, ASSETS / "model.onnx")
    tokenizer = download(base + "tokenizer.json", DEPS / "downloads/e5-ccc66d3-tokenizer.json")
    config = json.loads(tokenizer.read_text())
    # The application performs complete token windows itself; disable upstream
    # truncation/padding so a long passage cannot silently lose its tail.
    config["truncation"] = None
    config["padding"] = None
    (ASSETS / "tokenizer.json").write_text(json.dumps(config, ensure_ascii=False, separators=(",", ":")))
    if digest(ASSETS / "tokenizer.json") != LOCK["tokenizer_prepared_sha256"]:
        raise RuntimeError("Prepared tokenizer checksum mismatch; remove the cached tokenizer and run setup again.")
    for filename, url in {
        "tokenizers-LICENSE": "https://raw.githubusercontent.com/daulet/tokenizers/v1.27.0/LICENSE",
        "e5-MIT-LICENSE": "https://raw.githubusercontent.com/microsoft/unilm/master/LICENSE",
        "huggingface-tokenizers-LICENSE": "https://raw.githubusercontent.com/huggingface/tokenizers/v0.22.2/LICENSE",
    }.items():
        download(url, notices / filename)
    manifest = {
        "model_id": LOCK["model_id"],
        "model_source": base,
        "onnxruntime": LOCK["onnxruntime"],
        "tokenizers": LOCK["tokenizers"],
        "platform": key,
        "files": {name: digest(ASSETS / name) for name in ["model.onnx", "tokenizer.json", library]},
    }
    (ASSETS / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print("Native E5 assets ready: " + str(ASSETS))
    print(json.dumps(manifest, indent=2))

if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        sys.exit("setup: " + str(error))
