package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	autoStartValueName       = "LumeDAV"
	autoStartRunKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartApprovedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

type AutoStartStatus struct {
	Configured        bool   `json:"configured"`
	Registered        bool   `json:"registered"`
	Healthy           bool   `json:"healthy"`
	WindowsDisabled   bool   `json:"windowsDisabled"`
	ExpectedPath      string `json:"expectedPath"`
	RegisteredCommand string `json:"registeredCommand"`
	Message           string `json:"message"`
}

func hasLaunchArg(args []string, wanted string) bool {
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), wanted) {
			return true
		}
	}
	return false
}

func autoStartCommand(executable string) string {
	return `"` + filepath.Clean(executable) + `" --autostart`
}

func autoStartCommandMatches(actual, executable string) bool {
	return strings.EqualFold(strings.TrimSpace(actual), autoStartCommand(executable))
}

func currentExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(executable)
}

func inspectAutoStart(configured bool) AutoStartStatus {
	status := AutoStartStatus{Configured: configured}
	executable, err := currentExecutable()
	if err != nil {
		status.Message = "无法读取当前程序路径: " + err.Error()
		return status
	}
	status.ExpectedPath = executable

	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunKeyPath, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		command, _, readErr := key.GetStringValue(autoStartValueName)
		if readErr == nil {
			status.Registered = true
			status.RegisteredCommand = command
		} else if readErr != registry.ErrNotExist {
			status.Message = "读取 Windows 启动项失败: " + readErr.Error()
			return status
		}
	} else if err != registry.ErrNotExist {
		status.Message = "打开 Windows 启动项失败: " + err.Error()
		return status
	}

	approved, approvedErr := registry.OpenKey(registry.CURRENT_USER, autoStartApprovedKeyPath, registry.QUERY_VALUE)
	if approvedErr == nil {
		defer approved.Close()
		data, _, readErr := approved.GetBinaryValue(autoStartValueName)
		if readErr == nil && len(data) > 0 && data[0] == 3 {
			status.WindowsDisabled = true
		}
	}

	if !configured {
		status.Healthy = !status.Registered && !status.WindowsDisabled
		if status.Healthy {
			status.Message = "已关闭，Windows 中没有残留启动项"
		} else {
			status.Message = "配置已关闭，但 Windows 中仍有残留启动项"
		}
		return status
	}
	if status.WindowsDisabled {
		status.Message = "Windows 已禁用此启动项，可以一键修复"
		return status
	}
	if !status.Registered {
		status.Message = "Windows 启动项缺失，可以一键修复"
		return status
	}
	if !autoStartCommandMatches(status.RegisteredCommand, executable) {
		status.Message = "启动项仍指向旧版本或已移动的程序"
		return status
	}
	status.Healthy = true
	status.Message = "启动项正常，登录 Windows 后会在托盘运行"
	return status
}

func setAutoStart(enable bool) error {
	runKey, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartRunKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Windows Run 注册表失败: %w", err)
	}
	defer runKey.Close()

	if enable {
		executable, err := currentExecutable()
		if err != nil {
			return err
		}
		if err := runKey.SetStringValue(autoStartValueName, autoStartCommand(executable)); err != nil {
			return err
		}
	} else if err := runKey.DeleteValue(autoStartValueName); err != nil && err != registry.ErrNotExist {
		return err
	}

	// Clearing StartupApproved removes stale disabled metadata. The Run value
	// remains the single source of truth and avoids duplicate launch paths.
	approvedKey, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartApprovedKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Windows 启动审批项失败: %w", err)
	}
	defer approvedKey.Close()
	if err := approvedKey.DeleteValue(autoStartValueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}
