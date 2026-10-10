#!/usr/bin/env python3
"""Exercise real Settings saves, recreation, upgrade and offline-volume rollback.

Requires Linux Docker Engine and two locally built images. Uses only generated
fixtures and loopback HTTP; provider calls are never needed. Credentials and
container logs remain in memory and are never included in assertion failures.
"""
import argparse
import json
import re
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def docker(*args, check=True):
    """Capture Docker output privately; report failures without logs or secrets."""
    result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=180)
    if check and result.returncode:
        raise RuntimeError("Docker operation failed (exit %d)" % result.returncode)
    return result.stdout.strip()


class Regression:
    """Own only randomly named disposable test containers and volumes."""
    def __init__(self, current, baseline):
        self.current, self.baseline = current, baseline
        self.prefix = "ev-upgrade-" + uuid.uuid4().hex[:12]
        self.container = self.prefix + "-app"
        self.mounts = {"runtime": "/app/runtime", "data": "/app/data",
                       "log": "/app/log", "uploads": "/app/chat_uploads",
                       "knowledge": "/app/knowledge_base", "skills": "/app/skills",
                       "workspace": "/app/tmp"}
        self.volumes = {}
        self.backups = {}
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.token = None
        self.password = None

    def initialize(self):
        for key in self.mounts:
            self.volumes[key] = docker("volume", "create", self.prefix + "-" + key)
        # Initialize ownership from the current image, including directories that
        # did not exist in the baseline image (such as /app/tmp).
        args = ["run", "--rm", "--network", "none", "--entrypoint", "python3"]
        for key, path in self.mounts.items():
            args += ["-v", self.volumes[key] + ":" + path]
        # The historical baseline has a Settings YAML-node panic. Seed its
        # setting offline; only the fixed current image must save via the API.
        seed = """from pathlib import Path
import re
source = Path('/usr/local/share/cyberstrike-config.yaml').read_text()
source, count = re.subn(r'(?m)^  max_iterations:.*$', '  max_iterations: 37', source)
assert count == 1
target = Path('/app/runtime/config.yaml')
target.write_text(source)
target.chmod(0o600)
"""
        docker(*args, self.current, "-c", seed)

    def api(self, path, data=None, method=None):
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        request = urllib.request.Request(self.url + path,
            data=None if data is None else json.dumps(data).encode(), headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=10) as response:
                body = response.read()
                return json.loads(body) if body else None
        except urllib.error.HTTPError as error:
            raise RuntimeError("Fixture API returned HTTP %d" % error.code) from None

    def start(self, image):
        args = ["run", "-d", "--name", self.container, "--cap-drop", "ALL",
                "--security-opt", "no-new-privileges", "-p", "127.0.0.1::8080"]
        for key, path in self.mounts.items():
            args += ["-v", self.volumes[key] + ":" + path]
        args += [image]
        if image == self.baseline:
            args += ["--config", "/app/runtime/config.yaml", "--http"]
        docker(*args)
        port = docker("port", self.container, "8080/tcp").rsplit(":", 1)[1]
        self.url = "http://127.0.0.1:" + port
        for _ in range(90):
            try:
                with self.opener.open(self.url, timeout=2) as response:
                    if response.status == 200:
                        break
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(1)
        else:
            raise RuntimeError("Container did not become ready")
        if self.password is None:
            logs = re.sub(r"\x1b\[[0-9;]*m", "", docker("logs", self.container))
            match = re.search(r"Password\s+(\S+)", logs)
            if match is None:
                raise RuntimeError("Bootstrap credential was not produced")
            self.password = match.group(1)
        self.token = None
        self.token = self.api("/api/auth/login", {"username": "admin", "password": self.password})["token"]
        assert docker("exec", self.container, "id", "-u") == "10001", "Non-root identity changed"

    def stop(self):
        docker("stop", "--time", "45", self.container)
        docker("rm", self.container)
        self.token = None

    def copy_volume(self, source, destination):
        # Applications are stopped first. Copy all SQLite/WAL files and ownership
        # together; do not attempt live file-level database backups.
        docker("run", "--rm", "--network", "none", "--user", "0:0",
               "--entrypoint", "sh", "-v", source + ":/source:ro",
               "-v", destination + ":/destination", self.current,
               "-c", "cp -a /source/. /destination/")

    def snapshot(self):
        for key, source in self.volumes.items():
            self.backups[key] = docker("volume", "create", self.prefix + "-backup-" + key)
            self.copy_volume(source, self.backups[key])

    def restore(self):
        # Restore into new disposable volumes, leaving the snapshot untouched.
        for key, backup in self.backups.items():
            previous = self.volumes[key]
            self.volumes[key] = docker("volume", "create", self.prefix + "-restored-" + key)
            self.copy_volume(backup, self.volumes[key])
            docker("volume", "rm", previous)

    def verify(self, conversation_id, iterations):
        config = self.api("/api/config")
        assert config["agent"]["max_iterations"] == iterations, "Saved setting was not retained"
        conversation = self.api("/api/conversations/" + conversation_id)
        assert conversation["title"] == "container regression fixture", "Conversation was not retained"
        for key, path in self.mounts.items():
            assert docker("exec", self.container, "cat", path + "/.upgrade-fixture") == key, "Volume marker missing"

    def run(self):
        self.initialize()
        self.start(self.baseline)
        conversation_id = self.api("/api/conversations", {"title": "container regression fixture"})["id"]
        for key, path in self.mounts.items():
            docker("exec", self.container, "sh", "-c", 'printf "%s" "$1" > "$2/.upgrade-fixture"', "fixture", key, path)
        self.verify(conversation_id, 37)
        self.stop()
        self.snapshot()
        self.start(self.current)
        self.verify(conversation_id, 37)
        self.api("/api/config", {"agent": {"max_iterations": 41}}, "PUT")
        self.stop()
        self.start(self.current)
        self.verify(conversation_id, 41)
        self.stop()
        self.restore()
        self.start(self.baseline)
        self.verify(conversation_id, 37)
        self.stop()
        print("PASS: Settings save, upgrade, recreate and backup-based rollback; all seven volumes retained")

    def cleanup(self):
        docker("rm", "-f", self.container, check=False)
        for volume in {*self.volumes.values(), *self.backups.values()}:
            docker("volume", "rm", volume, check=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--current", required=True)
    parser.add_argument("--baseline", required=True)
    args = parser.parse_args()
    regression = Regression(args.current, args.baseline)
    try:
        regression.run()
    finally:
        regression.cleanup()


if __name__ == "__main__":
    main()
