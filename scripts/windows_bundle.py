"""Build and apply offline Windows bundles without copying runtime credentials.

Only tracked distributable resources are packaged. Upgrade verifies every member,
refuses running services/local modifications, and backs up changed managed files.
Python 3.11+; no third-party modules. Does not start or stop the service.
"""
import argparse
import contextlib
import ctypes
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

MANIFEST = 'release-manifest.json'
DIRECTORIES = {'web', 'roles', 'agents', 'skills'}
FILES = {'config.example.yaml', 'run.ps1', 'LICENSE', 'README.md', 'README_CN.md', 'docs/en-US/windows-native.md', 'docs/zh-CN/windows-native.md',
         'scripts/windows_bundle.py', 'scripts/install-windows-tool.ps1'}


def allowed(name):
    """Restrict bundle members to shipped code/resources, never live state."""
    path = PurePosixPath(name)
    if name != path.as_posix() or path.is_absolute() or any(p in ('', '.', '..') for p in name.split('/')):
        return False
    if '\\' in name or ':' in name or any(p.endswith((' ', '.')) for p in path.parts):
        return False
    if any(p.lower().split('.')[0] in {'con', 'prn', 'aux', 'nul', *('com%d' % n for n in range(1,10)), *('lpt%d' % n for n in range(1,10))} for p in path.parts):
        return False
    return name in FILES or name == 'bin/cyberstrike-ai.exe' or path.parts[0] in DIRECTORIES or (path.parts[0] == 'tools' and len(path.parts) == 2 and path.suffix == '.yaml')


def digest(data):
    """Return the exact bytes' SHA-256, used for integrity rather than signatures."""
    return hashlib.sha256(data).hexdigest()


def build(repository, binary, output):
    """Package HEAD resources and a separately verified native executable."""
    revision = subprocess.check_output(['git', '-C', str(repository), 'rev-parse', 'HEAD'], text=True).strip()
    archive = subprocess.check_output(['git', '-C', str(repository), 'archive', '--format=tar', 'HEAD'])
    members = {}
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        for member in source:
            if member.isfile() and allowed(member.name):
                members[member.name] = source.extractfile(member).read()
    data = binary.read_bytes()
    if data[:2] != b'MZ':
        raise ValueError('Windows executable required')
    members['bin/cyberstrike-ai.exe'] = data
    for required in ('config.example.yaml', 'run.ps1', 'scripts/windows_bundle.py'):
        if required not in members:
            raise ValueError('Missing committed package resource: ' + required)
    manifest = {'format': 1, 'platform': 'windows', 'revision': revision,
                'files': {name: digest(content) for name, content in sorted(members.items())}}
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, 'x', zipfile.ZIP_DEFLATED) as bundle:
        for name, content in members.items():
            bundle.writestr(name, content)
        bundle.writestr(MANIFEST, json.dumps(manifest, indent=2))
    output.with_suffix(output.suffix + '.sha256').write_text(digest(output.read_bytes()) + '  ' + output.name + '\n', encoding='ascii')
    return manifest


def read_bundle(package, expected):
    """Validate whole-archive checksum, member paths, duplicates and file hashes."""
    if not re.fullmatch('[a-fA-F0-9]{64}', expected) or digest(package.read_bytes()) != expected.lower():
        raise ValueError('Bundle SHA256 mismatch')
    with zipfile.ZipFile(package) as bundle:
        names = bundle.namelist()
        if len({name.casefold() for name in names}) != len(names):
            raise ValueError('Duplicate or case-colliding bundle member')
        manifest = json.loads(bundle.read(MANIFEST))
        if manifest.get('format') != 1 or manifest.get('platform') != 'windows' or not re.fullmatch('[0-9a-f]{40}', manifest.get('revision', '')):
            raise ValueError('Unsupported manifest')
        files = manifest['files']
        if set(names) != set(files) | {MANIFEST} or not all(allowed(name) for name in files):
            raise ValueError('Unexpected bundle member')
        if 'bin/cyberstrike-ai.exe' not in files:
            raise ValueError('Missing service executable')
        if sum(info.file_size for info in bundle.infolist()) > 1024**3:
            raise ValueError('Bundle exceeds 1 GiB uncompressed limit')
        contents = {name: bundle.read(name) for name in files}
        if any(digest(content) != files[name] for name, content in contents.items()):
            raise ValueError('Member checksum mismatch')
        contents[MANIFEST] = bundle.read(MANIFEST)
        return contents


def safe_target(root, name):
    """Reject symlinks/junctions and files that would escape the installation."""
    path = root / name
    for parent in (path, *path.parents):
        if parent.is_symlink() or (parent.exists() and getattr(parent.stat(), 'st_file_attributes', 0) & 0x400):
            raise ValueError('Linked installation paths are unsupported')
    return path


@contextlib.contextmanager
def stopped_service(binary):
    """Keep an exclusive executable handle during upgrade; a running service fails."""
    if not binary.exists():
        yield
        return
    if os.name != 'nt':
        raise ValueError('Applying a Windows upgrade requires Windows')
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.CreateFileW.argtypes = [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_void_p, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_void_p]
    kernel.CreateFileW.restype = ctypes.c_void_p
    # READ sharing permits backup reads; the retained write handle prevents
    # image loading. DELETE sharing allows our atomic replacement.
    handle = kernel.CreateFileW(str(binary), 0xC0000000, 5, None, 3, 0, None)
    if handle == ctypes.c_void_p(-1).value:
        raise OSError('Stop the service before upgrade; executable is busy or inaccessible')
    kernel.CloseHandle.argtypes = [ctypes.c_void_p]
    try:
        yield
    finally:
        kernel.CloseHandle(handle)


def apply(package, expected, target):
    """Apply a verified bundle transactionally, preserving non-managed data.

    Existing managed files must match the previous manifest (or new bytes on a
    first install). Conflicts abort before mutation. Backups remain for rollback.
    """
    if os.name != 'nt':
        raise ValueError('Apply requires Windows; build/verify work on any platform')
    contents = read_bundle(package, expected)
    root = target.absolute()
    safe_target(root, MANIFEST)
    root.mkdir(parents=True, exist_ok=True)
    lock = safe_target(root, '.upgrade.lock')
    # Exclusive creation serializes upgrades; the launcher respects this lock.
    with lock.open('x', encoding='ascii') as stream:
        stream.write(str(os.getpid()))
    try:
        return apply_verified(contents, root)
    finally:
        lock.unlink()


def apply_verified(contents, root):
    """Replace managed resources while the caller owns the installation lock."""
    safe_target(root, MANIFEST)
    old_manifest = root / MANIFEST
    previous = json.loads(old_manifest.read_text(encoding='utf-8'))['files'] if old_manifest.exists() else {}
    if not all(allowed(name) for name in previous):
        raise ValueError('Invalid previous manifest paths')
    # Superseded managed files are removed only if still identical to old hashes.
    names = sorted(set(contents) | set(previous))
    for name in names:
        path = safe_target(root, name)
        if path.exists() and name != MANIFEST:
            expected_old = previous.get(name, digest(contents[name]) if name in contents else None)
            if not path.is_file() or digest(path.read_bytes()) != expected_old:
                raise ValueError('Locally modified managed file: ' + name)
    root.mkdir(parents=True, exist_ok=True)
    with stopped_service(root / 'bin/cyberstrike-ai.exe'):
        backup = Path(tempfile.mkdtemp(prefix=root.name + '-backup-', dir=root.parent))
        changed = []
        try:
            for name in names:
                path = safe_target(root, name)
                saved = backup / name
                if path.exists():
                    saved.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(path, saved)
                changed.append(name)
                if name not in contents:
                    path.unlink()
                    continue
                path.parent.mkdir(parents=True, exist_ok=True)
                with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as temporary:
                    temporary.write(contents[name])
                    temporary_path = Path(temporary.name)
                try:
                    os.replace(temporary_path, path)
                finally:
                    temporary_path.unlink(missing_ok=True)
        except Exception:
            for name in reversed(changed):
                path, saved = root / name, backup / name
                if saved.exists():
                    shutil.copy2(saved, path)
                else:
                    path.unlink(missing_ok=True)
            raise
    return backup


def main():
    """Expose explicit build, verify and stopped-service apply commands."""
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    build_parser = sub.add_parser('build')
    build_parser.add_argument('--repository', type=Path, default=Path.cwd())
    build_parser.add_argument('--binary', type=Path, required=True)
    build_parser.add_argument('--output', type=Path, required=True)
    for action in ('verify', 'apply'):
        command = sub.add_parser(action)
        command.add_argument('--package', type=Path, required=True)
        command.add_argument('--sha256', required=True)
        if action == 'apply':
            command.add_argument('--target', type=Path, required=True)
    args = parser.parse_args()
    if args.action == 'build':
        print('Packaged revision', build(args.repository, args.binary, args.output)['revision'])
    elif args.action == 'verify':
        print('Verified files:', len(read_bundle(args.package, args.sha256)))
    else:
        print('Updated. Managed-file backup:', apply(args.package, args.sha256, args.target))


if __name__ == '__main__':
    main()
