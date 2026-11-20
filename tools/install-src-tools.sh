#!/usr/bin/env bash
# Install SRC dependencies inside tools/runtime on Ubuntu; never uses sudo.
# Usage: bash tools/install-src-tools.sh. Existing installations are reused.
# A failed download/build stops installation; logs and downloaded packages remain for diagnosis.
set -euo pipefail
umask 077

tools_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
runtime_dir="$tools_root/runtime"
mkdir -p "$runtime_dir" "$runtime_dir/bin" "$runtime_dir/cache" "$runtime_dir/logs"
exec > >(tee -a "$runtime_dir/logs/install-src-tools.log") 2>&1
for dependency in python3 curl git go apt-get dpkg-deb; do
    command -v "$dependency" >/dev/null || { echo "Required installer dependency missing: $dependency"; exit 1; }
done

echo 'Preparing project-local Python dependencies'
python3 -m venv --system-site-packages "$runtime_dir/python"
"$runtime_dir/python/bin/python3" -m pip install --disable-pip-version-check 'httpx[http2,socks]==0.28.1' 'requests==2.33.0' 'charset-normalizer==3.4.4' 'PyYAML==6.0.3' 'termcolor==3.3.0' 'cprint==1.2.2' 'pycryptodomex==3.23.0' 'ratelimit==2.2.1'

jwt_revision=3bc7407cf2222d6a821dcc19c776e5a1b1cb9a9b
if [ ! -d "$runtime_dir/jwt-tool/.git" ]; then
    git init "$runtime_dir/jwt-tool"
    git -C "$runtime_dir/jwt-tool" remote add origin https://github.com/ticarpi/jwt_tool.git
    git -C "$runtime_dir/jwt-tool" fetch --depth 1 origin "$jwt_revision"
    git -C "$runtime_dir/jwt-tool" checkout --detach FETCH_HEAD
fi
test "$(git -C "$runtime_dir/jwt-tool" rev-parse HEAD)" = "$jwt_revision"
mkdir -p "$runtime_dir/jwt-home"
"$runtime_dir/python/bin/python3" "$tools_root/scripts/prepare-jwt-runtime.py" "$runtime_dir"

echo 'Preparing project-local Node and Spectral'
node_version=v24.18.1
case "$(uname -m)" in
    x86_64) node_arch=x64 ;;
    aarch64|arm64) node_arch=arm64 ;;
    *) echo 'Supported architectures: x86_64 and arm64'; exit 1 ;;
esac
node_archive="node-$node_version-linux-$node_arch.tar.xz"
if [ ! -x "$runtime_dir/node/bin/node" ]; then
    curl --fail --location --retry 3 --retry-all-errors --connect-timeout 20 --max-time 300 --proto '=https' -o "$runtime_dir/cache/$node_archive" "https://nodejs.org/dist/$node_version/$node_archive"
    curl --fail --location --retry 3 --retry-all-errors --connect-timeout 20 --max-time 300 --proto '=https' -o "$runtime_dir/cache/SHASUMS256.txt" "https://nodejs.org/dist/$node_version/SHASUMS256.txt"
    python3 - "$runtime_dir/cache" "$node_archive" "$runtime_dir" <<'PY'
import hashlib, pathlib, sys, tarfile
cache, name, runtime = pathlib.Path(sys.argv[1]), sys.argv[2], pathlib.Path(sys.argv[3])
entries = dict((line.split()[1], line.split()[0]) for line in (cache/'SHASUMS256.txt').read_text().splitlines())
if hashlib.sha256((cache/name).read_bytes()).hexdigest() != entries.get(name):
    raise SystemExit('Node archive checksum mismatch')
with tarfile.open(cache/name) as archive:
    archive.extractall(runtime/'cache'/'node-extract', filter='data')
source = runtime/'cache'/'node-extract'/name.removesuffix('.tar.xz')
source.rename(runtime/'node')
PY
fi
export PATH="$runtime_dir/node/bin:$PATH"
export npm_config_cache="$runtime_dir/cache/npm"
npm install --prefix "$runtime_dir/spectral" --ignore-scripts --no-fund --no-audit --save-exact '@stoplight/spectral-cli@6.17.0'
printf '%s\n' 'extends: [spectral:oas]' > "$runtime_dir/spectral-ruleset.yaml"

echo 'Preparing project-local Nmap from Ubuntu package metadata'
if [ ! -f "$runtime_dir/nmap/.dependencies-ready" ]; then
    mkdir -p "$runtime_dir/cache/nmap" "$runtime_dir/nmap"
    (
        cd "$runtime_dir/cache/nmap"
        apt-get download nmap nmap-common liblinear4 liblua5.4-0 libpcre2-8-0 libblas3 liblapack3 libgfortran5 libquadmath0
        for package in ./*.deb; do
            dpkg-deb --extract "$package" "$runtime_dir/nmap"
        done
    )
    touch "$runtime_dir/nmap/.dependencies-ready"
fi

echo 'Preparing project-local Waybackurls'
if [ ! -x "$runtime_dir/bin/waybackurls" ]; then
    GOBIN="$runtime_dir/bin" GOMODCACHE="$runtime_dir/cache/go-mod" GOCACHE="$runtime_dir/cache/go-build" go install github.com/tomnomnom/waybackurls@v0.1.0
fi

# These launchers resolve their own directory; moving the project does not embed host paths.
cat > "$runtime_dir/bin/python3" <<'SH'
#!/bin/sh
runtime_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec "$runtime_dir/python/bin/python3" "$@"
SH
cat > "$runtime_dir/bin/jwt_tool" <<'SH'
#!/bin/sh
runtime_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
HOME="$runtime_dir/jwt-home" exec "$runtime_dir/python/bin/python3" "$runtime_dir/jwt-tool/jwt_tool.py" "$@"
SH
cat > "$runtime_dir/bin/spectral" <<'SH'
#!/bin/sh
runtime_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "${1:-}" = lint ]; then
    exec "$runtime_dir/node/bin/node" "$runtime_dir/spectral/node_modules/@stoplight/spectral-cli/dist/index.js" "$@" --ruleset "$runtime_dir/spectral-ruleset.yaml"
fi
exec "$runtime_dir/node/bin/node" "$runtime_dir/spectral/node_modules/@stoplight/spectral-cli/dist/index.js" "$@"
SH
cat > "$runtime_dir/bin/nmap" <<'SH'
#!/bin/sh
runtime_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
libs=$(find "$runtime_dir/nmap/usr/lib" -type d \( -name '*linux-gnu' -o -name blas -o -name lapack \) -print | paste -sd : -)
LD_LIBRARY_PATH="$libs${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" NMAPDIR="$runtime_dir/nmap/usr/share/nmap" exec "$runtime_dir/nmap/usr/bin/nmap" "$@"
SH
chmod 700 "$runtime_dir/bin/python3" "$runtime_dir/bin/jwt_tool" "$runtime_dir/bin/spectral" "$runtime_dir/bin/nmap"

echo 'Checking installed tool entry points (no target traffic)'
"$runtime_dir/bin/python3" -c 'import httpx, requests, yaml, Cryptodome; print("Python dependencies ready")'
"$runtime_dir/bin/jwt_tool" --help >/dev/null
"$runtime_dir/bin/spectral" --version
"$runtime_dir/bin/nmap" --version
"$runtime_dir/bin/waybackurls" -h >/dev/null
"$runtime_dir/python/bin/python3" -m pip freeze --local > "$runtime_dir/python-requirements.lock.txt"
sha256sum "$runtime_dir/bin/waybackurls" "$runtime_dir/node/bin/node" "$runtime_dir/nmap/usr/bin/nmap" > "$runtime_dir/binaries.sha256"
echo 'SRC tool installation and entry point checks completed.'
