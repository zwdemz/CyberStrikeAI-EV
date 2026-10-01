"""Prepare the pinned JWT tool for Python 3.14 and verify offline decoding.

Usage: python prepare-jwt-runtime.py RUNTIME_DIRECTORY. Existing valid tool state
is retained. Unexpected source edits or decoding failures stop installation.
"""

import configparser
import os
from pathlib import Path
import subprocess
import sys
import time


def prepare_runtime(runtime: Path) -> None:
    """Apply checked compatibility changes and decode a nonprivileged fixture.

    Runtime must contain the pinned jwt-tool checkout and project venv. Unknown
    local edits raise RuntimeError; incomplete initial state is backed up.
    """
    checkout = runtime / "jwt-tool"
    entry = checkout / "jwt_tool.py"
    original = subprocess.check_output(["git", "-C", str(checkout), "show", "HEAD:jwt_tool.py"])
    old = b"configparser.ConfigParser(allow_no_value=True)"
    new = b"configparser.ConfigParser(allow_no_value=True, delimiters=('=',))"
    if original.count(old) != 1:
        raise RuntimeError("JWT configuration initializer no longer matches the compatibility patch")
    config_patched = original.replace(old, new, 1)
    success_exit = b"    runActions()\n    exit(1)"
    if config_patched.count(success_exit) != 1:
        raise RuntimeError("JWT successful exit no longer matches the compatibility patch")
    # The pinned upstream version exits 1 even after successful offline decoding.
    # Change only the final success path; explicit validation failures stay errors.
    patched = config_patched.replace(success_exit, b"    runActions()\n    exit(0)", 1)
    if entry.read_bytes() not in (original, config_patched, patched):
        raise RuntimeError("JWT source has unexpected local changes; preserve and review them before installing")
    # Upstream stores comments as no-value keys containing colons. Python 3.14
    # validates delimiters on write; the generated INI uses '=' for every value.
    entry.write_bytes(patched)
    config_file = runtime / "jwt-home" / ".jwt_tool" / "jwtconf.ini"
    required = {"crypto", "customising", "services", "input", "argvals"}
    parser = configparser.ConfigParser()
    if config_file.exists():
        try:
            parser.read(config_file)
            complete = required.issubset(parser.sections())
        except configparser.Error:
            complete = False
        if not complete:
            config_file.rename(config_file.with_name(f"jwtconf.incomplete-{time.time_ns()}.ini"))
    environment = dict(os.environ, HOME=str(runtime / "jwt-home"))
    token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJmaXh0dXJlIn0.c2lnbmF0dXJl"
    command = [str(runtime / "python" / "bin" / "python3"), str(entry), token]
    result = subprocess.run(command, cwd=checkout, env=environment, capture_output=True, text=True, timeout=60)
    # Upstream intentionally exits 1 after creating a configuration for the first time.
    if result.returncode == 1 and config_file.exists():
        result = subprocess.run(command, cwd=checkout, env=environment, capture_output=True, text=True, timeout=60)
    (runtime / "logs" / "jwt-smoke.log").write_text(result.stdout + result.stderr)
    if result.returncode != 0 or "fixture" not in result.stdout:
        raise RuntimeError("JWT offline fixture failed; inspect tools/runtime/logs/jwt-smoke.log")
    invalid = subprocess.run(command[:-1] + ["invalid-token"], cwd=checkout, env=environment, capture_output=True, timeout=30)
    if invalid.returncode == 0:
        raise RuntimeError("JWT invalid input unexpectedly succeeded")


if __name__ == "__main__":
    prepare_runtime(Path(sys.argv[1]).resolve())
