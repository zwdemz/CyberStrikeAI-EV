package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceAddressedWorkerRejectsStaleBundleAndSupportsRollback(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	runtimeRoot := filepath.Join(root, "runtime", "pi")
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "package-lock.json", "tsconfig.json"} {
		if err := os.WriteFile(filepath.Join(runtimeRoot, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(runtimeRoot, "src", "worker.ts")
	os.WriteFile(source, []byte("version one"), 0o600)
	hash, err := SourceFingerprint(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(runtimeRoot, ".builds", hash)
	os.MkdirAll(filepath.Join(bundle, "dist"), 0o700)
	os.WriteFile(filepath.Join(bundle, "dist", "worker.js"), []byte("worker"), 0o600)
	if _, err := ResolveWorker(""); err == nil {
		t.Fatal("unpublished bundle was selected")
	}
	os.WriteFile(filepath.Join(bundle, ".ready"), []byte(hash), 0o600)
	worker, err := ResolveWorker("")
	if err != nil {
		t.Fatal(err)
	}
	if worker != filepath.Join(bundle, "dist", "worker.js") {
		t.Fatalf("wrong worker: %s", worker)
	}
	os.WriteFile(source, []byte("version two"), 0o600)
	if _, err := ResolveWorker(""); err == nil {
		t.Fatal("stale source bundle was selected")
	}
	os.WriteFile(source, []byte("version one"), 0o600)
	if restored, err := ResolveWorker(""); err != nil || restored != worker {
		t.Fatalf("rollback lost original bundle: %s %v", restored, err)
	}
}

// Text checkout line endings must not invalidate a worker shipped in an offline bundle.
func TestSourceFingerprintMatchesWindowsAndLinuxText(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "src"), 0700)
	for _, name := range []string{"package.json", "package-lock.json", "tsconfig.json", "src/worker.ts"} {
		os.WriteFile(filepath.Join(root, name), []byte("first\r\nsecond\r\n"), 0600)
	}
	windows, err := SourceFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "package-lock.json", "tsconfig.json", "src/worker.ts"} {
		os.WriteFile(filepath.Join(root, name), []byte("first\nsecond\n"), 0600)
	}
	linux, err := SourceFingerprint(root)
	if err != nil || windows != linux {
		t.Fatalf("line endings changed identity: %s %s %v", windows, linux, err)
	}
}

func TestConfiguredNodeSelectsItsOwnNPMWithoutPATHLookup(t *testing.T) {
	for _, layout := range []string{"nvm", "windows"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			node := filepath.Join(root, "bin", "node")
			cli := filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js")
			if layout == "windows" {
				cli = filepath.Join(root, "bin", "node_modules", "npm", "bin", "npm-cli.js")
			}
			if err := os.MkdirAll(filepath.Dir(cli), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cli, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := piNPMCommand(context.Background(), node, "ci", "--ignore-scripts")
			if cmd.Path != node || len(cmd.Args) != 4 || filepath.Clean(cmd.Args[1]) != filepath.Clean(cli) || cmd.Args[3] != "--ignore-scripts" {
				t.Fatalf("wrong command: %q %v", cmd.Path, cmd.Args)
			}
		})
	}
}
