package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeUpdateRunningImageGuard(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Root: filepath.Dir(executable)}
	if runtime.GOOS != "windows" {
		if err := refuseRunningWindowsInstall(opts); err != nil {
			t.Fatal(err)
		}
		return
	}
	for name, mutate := range map[string]func() error{
		"apply":    func() error { _, err := Apply(context.Background(), opts, nil); return err },
		"adopt":    func() error { _, err := Adopt(context.Background(), opts, nil); return err },
		"rollback": func() error { _, err := Rollback(context.Background(), opts); return err },
	} {
		t.Run(name, func(t *testing.T) {
			var failure *Error
			if err := mutate(); !errors.As(err, &failure) || failure.Reason != "running_windows_install" {
				t.Fatalf("expected pre-mutation Windows guard, got %v", err)
			}
		})
	}
	if err := refuseRunningWindowsInstall(Options{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}
