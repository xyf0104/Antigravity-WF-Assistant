//go:build windows

package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	winapi "golang.org/x/sys/windows"
)

const windowsCloseMessage = 0x0010 // WM_CLOSE

var (
	user32Process             = winapi.NewLazySystemDLL("user32.dll")
	enumWindowsProcess        = user32Process.NewProc("EnumWindows")
	getWindowThreadProcessID  = user32Process.NewProc("GetWindowThreadProcessId")
	postMessageWindowsProcess = user32Process.NewProc("PostMessageW")
)

func platformSupported() bool { return true }

func platformIsRunning(installRoot string) (bool, error) {
	root, err := validateWindowsInstallRoot(installRoot)
	if err != nil {
		return false, err
	}
	pids, err := windowsPIDsInRoot(root)
	return len(pids) > 0, err
}

func platformQuitGracefully(installRoot string, timeout time.Duration) error {
	root, err := validateWindowsInstallRoot(installRoot)
	if err != nil {
		return err
	}
	pids, err := windowsPIDsInRoot(root)
	if err != nil || len(pids) == 0 {
		return err
	}
	requested, err := requestWindowsClose(pids)
	if err != nil {
		return err
	}
	if requested == 0 {
		return fmt.Errorf("%s 没有可正常关闭的主窗口；已停止重启且不会强制结束进程，请先在 Antigravity 内保存内容并手动退出", filepath.Base(root))
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := platformIsRunning(root)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("%s 未在 %s 内正常退出；已停止重启且不会强制结束进程，请处理未保存内容后重试", filepath.Base(root), timeout.Round(time.Second))
}

func platformLaunch(executablePath string) error {
	executable, err := validateWindowsExecutable(executablePath)
	if err != nil {
		return err
	}
	cmd := exec.Command(executable)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", filepath.Base(executable), err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("启动 %s 后释放进程句柄失败: %w", filepath.Base(executable), err)
	}
	return nil
}

func platformWaitUntilRunning(installRoot string, timeout time.Duration) error {
	root, err := validateWindowsInstallRoot(installRoot)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := platformIsRunning(root)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("%s 启动超时，请从开始菜单手动打开并检查系统提示", filepath.Base(root))
}

func validateWindowsInstallRoot(value string) (string, error) {
	clean := filepath.Clean(strings.Trim(strings.TrimSpace(value), `"`))
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("解析安装路径失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("安装目录不存在: %s", abs)
	}
	return abs, nil
}

func validateWindowsExecutable(value string) (string, error) {
	clean := filepath.Clean(strings.Trim(strings.TrimSpace(value), `"`))
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("解析可执行文件失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(abs), ".exe") {
		return "", fmt.Errorf("Antigravity 可执行文件不存在: %s", abs)
	}
	return abs, nil
}

func windowsPIDsInRoot(root string) ([]uint32, error) {
	snapshot, err := winapi.CreateToolhelp32Snapshot(winapi.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("读取 Windows 进程列表失败: %w", err)
	}
	defer winapi.CloseHandle(snapshot)

	entry := winapi.ProcessEntry32{Size: uint32(unsafe.Sizeof(winapi.ProcessEntry32{}))}
	if err := winapi.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, winapi.ERROR_NO_MORE_FILES) {
			return nil, nil
		}
		return nil, err
	}
	var pids []uint32
	for {
		if entry.ProcessID != 0 {
			if image, ok := windowsProcessImage(entry.ProcessID); ok && windowsPathWithinRoot(image, root) {
				pids = append(pids, entry.ProcessID)
			}
		}
		if err := winapi.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, winapi.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, err
		}
	}
	return pids, nil
}

func windowsProcessImage(pid uint32) (string, bool) {
	handle, err := winapi.OpenProcess(winapi.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer winapi.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := winapi.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", false
	}
	return winapi.UTF16ToString(buffer[:size]), true
}

func requestWindowsClose(pids []uint32) (int, error) {
	targets := make(map[uint32]struct{}, len(pids))
	for _, pid := range pids {
		targets[pid] = struct{}{}
	}
	requested := 0
	callback := winapi.NewCallback(func(window uintptr, _ uintptr) uintptr {
		var pid uint32
		getWindowThreadProcessID.Call(window, uintptr(unsafe.Pointer(&pid)))
		if _, ok := targets[pid]; ok {
			if result, _, _ := postMessageWindowsProcess.Call(window, windowsCloseMessage, 0, 0); result != 0 {
				requested++
			}
		}
		return 1
	})
	result, _, callErr := enumWindowsProcess.Call(callback, 0)
	if result == 0 {
		return requested, fmt.Errorf("请求 Antigravity 正常退出失败: %w", callErr)
	}
	return requested, nil
}
