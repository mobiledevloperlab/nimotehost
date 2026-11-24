#!/usr/bin/env python3
"""Build desktop distributions and publish binaries plus SHA1 under release/."""
import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def run(*args):
    subprocess.run(args, cwd=ROOT, check=True, env={**os.environ, "GOWORK": "off"})


def sha1(path):
    digest = hashlib.sha1()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="2.5.0-community")
    parser.add_argument("--os", choices=["darwin", "linux", "windows", "all"],
                        help="default: current OS; all requires --cli-only")
    parser.add_argument("--arch", default="386,amd64,arm64",
                        help="comma separated: amd64 (x64), arm64, 386 (x86 CLI only)")
    parser.add_argument("--cli-only", action="store_true")
    args = parser.parse_args()
    native_os, native_arch = subprocess.check_output(
        ["go", "env", "GOHOSTOS", "GOHOSTARCH"], cwd=ROOT, text=True).split()
    systems = ["linux", "darwin", "windows"] if args.os == "all" else [args.os or native_os]
    arches = list(dict.fromkeys(args.arch.split(",")))
    if not arches or any(a not in ("amd64", "arm64", "386") for a in arches):
        parser.error("--arch must contain amd64, arm64 and/or 386")
    targets = [(s, a) for s in systems for a in arches if not (s == "darwin" and a == "386")]
    if not targets:
        parser.error("macOS does not support 386")
    if not args.cli_only:
        for system, arch in targets:
            if arch == "386":
                continue
            if system != native_os or (arch != native_arch and system != "darwin"):
                parser.error(f"GUI {system}/{arch} requires its native runner and desktop SDK; "
                             "use --arch for the native architecture or --cli-only")
    release = ROOT / "release"
    release.mkdir(exist_ok=True)
    # Stage the entire invocation before publishing; failed builds never replace releases.
    with tempfile.TemporaryDirectory(prefix=".build-", dir=release) as temporary:
        stage = Path(temporary)
        for system, arch in targets:
            gui = not args.cli_only and arch != "386"
            command = ["go", "run", "./tools/build", "-os", system,
                       "-arch", arch, "-version", args.version]
            if gui:
                command.append("-gui")
            print(f"Building {system}/{arch}: CLI" + (" + GUI" if gui else ""), flush=True)
            run(*command)
            for edition in (["cli", "gui"] if gui else ["cli"]):
                prefix = "gui-" if edition == "gui" else ""
                source = ROOT / "dist" / f"{prefix}{system}-{arch}"
                destination = stage / f"nimotehost-{edition}-{args.version}-{system}-{arch}"
                shutil.copytree(source, destination)
                # Hash every bundled file, including the actual executable and app contents.
                files = sorted(p for p in destination.rglob("*") if p.is_file())
                manifest = "".join(f"{sha1(p)}  {p.relative_to(destination).as_posix()}\n"
                                   for p in files)
                (destination / "SHA1SUMS").write_text(manifest, encoding="utf-8")
                for path in files:
                    if path.name in ("nimotehost", "nimotehost.exe", "nimotehost-gui", "nimotehost-gui.exe"):
                        path.with_name(path.name + ".sha1").write_text(
                            f"{sha1(path)}  {path.name}\n", encoding="utf-8")
        for source in sorted(stage.iterdir()):
            destination = release / source.name
            if destination.exists():
                shutil.rmtree(destination)
            source.rename(destination)
            print(destination)
    print("Release complete: binaries, .sha1 and SHA1SUMS are in", release)


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, OSError) as error:
        raise SystemExit(f"Release failed: {error}")
