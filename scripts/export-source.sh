#!/usr/bin/env bash
# Export only standalone public source, without parent files or Git history.
set -euo pipefail
host_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$host_repo_root" "${1:-$host_repo_root/dist/public/nimotehost-source.tar.gz}" <<'PY'
import gzip, io, pathlib, re, subprocess, sys, tarfile
root = pathlib.Path(sys.argv[1]); source = root
# Explicit top-level allowlist excludes build outputs and all parent files.
entries = ['README.zh-CN.md', 'CONTRIBUTING.zh-CN.md', 'SECURITY.zh-CN.md',
           'CHANGELOG.zh-CN.md', 'THIRD_PARTY_NOTICES.zh-CN.md',
           'README.md', 'LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md', 'assets',
           'CONTRIBUTING.md', 'SECURITY.md', 'CHANGELOG.md', 'Makefile',
           '.gitignore', '.github', 'go.mod', 'go.sum', 'cmd', 'internal',
           'scripts', 'docs', 'tools', 'third_party', 'test', 'pkg', 'examples', 'plugin-catalog']
# Worktrees can carry private local notes under an otherwise public directory.
# Source archives already contain only the exported tree.
tracked = None
if (root / '.git').exists() or (root / '.git').is_symlink():
    index = subprocess.run(['git', 'ls-files', '-z'], cwd=root, capture_output=True, check=True)
    tracked = set(index.stdout.decode().split('\0'))
files = []
secret = re.compile(rb'-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|sk-(?:proj-|ant-)?[A-Za-z0-9_-]{32,}')
for entry in entries:
    item = source / entry
    if item.is_symlink(): raise SystemExit(f'Symlink excluded: {entry}')
    if not item.exists(): raise SystemExit(f'Missing public source entry: {entry}')
    if tracked is None:
        paths = [item] + sorted(item.rglob('*')) if item.is_dir() else [item]
    else:
        paths = [source / name for name in tracked if name == entry or name.startswith(entry + '/')]
        if not paths: raise SystemExit(f'No tracked public source for: {entry}')
    for path in paths:
        relative = path.relative_to(source)
        if '__pycache__' in relative.parts or path.suffix in {'.pyc', '.pyo'}: continue
        if path.is_symlink() or any((source / parent).is_symlink() for parent in relative.parents):
            raise SystemExit(f'Symlink excluded: {relative}')
        if '.git' in path.relative_to(source).parts: raise SystemExit('Git metadata excluded')
        if path.is_dir(): continue
        if path.name.startswith('.env') or path.suffix.lower() in {'.pem', '.key', '.p12', '.pfx'}:
            raise SystemExit(f'Credential file excluded: {path.relative_to(source)}')
        data = path.read_bytes()
        if secret.search(data): raise SystemExit(f'Credential pattern found in {path.relative_to(source)}')
        files.append((path.relative_to(source).as_posix(), data, path.stat().st_mode))
output = pathlib.Path(sys.argv[2])
output.parent.mkdir(parents=True, exist_ok=True)
with output.open('wb') as dest, gzip.GzipFile(filename='', mode='wb', fileobj=dest, mtime=0) as compressed, tarfile.open(fileobj=compressed, mode='w') as archive:
    for name, data, mode in sorted(files):
        info = tarfile.TarInfo(name); info.size = len(data)
        info.mode = 0o755 if mode & 0o111 else 0o644
        info.mtime = 0; info.uid = info.gid = 0
        archive.addfile(info, io.BytesIO(data))
print(output)
PY
