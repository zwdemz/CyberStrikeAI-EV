package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func runtimeSourceFiles(root string) ([]string, error) {
	files := []string{"package.json", "package-lock.json", "tsconfig.json"}
	err := filepath.WalkDir(filepath.Join(root, "src"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime source must not be a symlink: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func SourceFingerprint(root string) (string, error) {
	files, err := runtimeSourceFiles(root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, file := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return "", err
		}
		h.Write([]byte(file))
		h.Write([]byte{0})
		// Source text is equivalent across Git's Windows/Linux checkout line endings.
		// Package manifests still verify exact deployed bytes independently.
		h.Write(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// PreparePiRuntime publishes an immutable source-addressed worker bundle. Source rollback
// selects the prior bundle automatically; an active worker keeps its original dependencies.
func PreparePiRuntime(ctx context.Context, root string) (string, error) {
	return preparePiRuntime(ctx, root, "")
}

func preparePiRuntime(ctx context.Context, root, node string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fingerprint, err := SourceFingerprint(root)
	if err != nil {
		return "", err
	}
	cache := filepath.Join(root, ".builds")
	dest := filepath.Join(cache, fingerprint)
	worker := filepath.Join(dest, "dist", "worker.js")
	if _, err := os.Stat(filepath.Join(dest, ".ready")); err == nil {
		if _, err = os.Stat(worker); err == nil {
			return worker, nil
		}
	}
	if err = os.MkdirAll(cache, 0o700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(cache, ".staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	files, err := runtimeSourceFiles(root)
	if err != nil {
		return "", err
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return "", err
		}
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err = os.WriteFile(path, body, 0o600); err != nil {
			return "", err
		}
	}
	if node == "" {
		node = "node"
	}
	if node, err = exec.LookPath(node); err != nil {
		return "", fmt.Errorf("Pi requires Node.js >=22.19: %w", err)
	}
	check := exec.CommandContext(ctx, node, "-e", `const [a,b]=process.versions.node.split('.').map(Number);process.exit(a>22||(a===22&&b>=19)?0:1)`)
	if err = check.Run(); err != nil {
		return "", fmt.Errorf("Pi requires Node.js >=22.19")
	}
	for _, args := range [][]string{{"ci", "--ignore-scripts", "--no-audit", "--no-fund"}, {"run", "build"}} {
		cmd := piNPMCommand(ctx, node, args...)
		cmd.Dir = stage
		cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(node)+string(os.PathListSeparator)+os.Getenv("PATH"))
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err = cmd.Run(); err != nil {
			msg := strings.TrimSpace(output.String())
			if len(msg) > 4000 {
				msg = msg[len(msg)-4000:]
			}
			return "", fmt.Errorf("Pi runtime build failed: %w\n%s", err, msg)
		}
	}
	if _, err = os.Stat(filepath.Join(stage, "dist", "worker.js")); err != nil {
		return "", fmt.Errorf("Pi build produced no worker: %w", err)
	}
	if err = os.WriteFile(filepath.Join(stage, ".ready"), []byte(fingerprint), 0o600); err != nil {
		return "", err
	}
	if err = os.Rename(stage, dest); err != nil {
		if _, exists := os.Stat(filepath.Join(dest, ".ready")); exists == nil {
			return worker, nil
		}
		return "", err
	}
	return worker, nil
}

// piNPMCommand prefers the npm CLI belonging to the configured Node installation.
// This supports NVM and Windows services whose PATH omits the interactive user's
// Node directory. System npm remains the fallback for distro-managed installs.
func piNPMCommand(ctx context.Context, node string, args ...string) *exec.Cmd {
	for _, cli := range []string{
		filepath.Join(filepath.Dir(node), "node_modules", "npm", "bin", "npm-cli.js"),
		filepath.Join(filepath.Dir(node), "..", "lib", "node_modules", "npm", "bin", "npm-cli.js"),
	} {
		if info, err := os.Stat(cli); err == nil && info.Mode().IsRegular() {
			return exec.CommandContext(ctx, node, append([]string{cli}, args...)...)
		}
	}
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd.exe", append([]string{"/d", "/c", "npm"}, args...)...)
	}
	return exec.CommandContext(ctx, "npm", args...)
}
