package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// refuseRunningWindowsInstall rejects mutation before Git changes the tree when
// the updater is running from that installation. Windows locks a running image;
// use the offline bundle procedure after stopping the service instead. Inspection
// and updating a separate, stopped installation remain available.
func refuseRunningWindowsInstall(opts Options) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	root, err := opts.installRoot()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, executable)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return &Error{Reason: "running_windows_install", Message: "Windows 正在运行此安装目录。请等待任务结束、停止服务，再使用离线升级包；检查更新仍可使用。"}
	}
	return nil
}
