package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"antigravity-byok/internal/totp"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TOTPStatus never contains a Base32 secret. It is safe for Wails to marshal
// into the renderer because only user-visible metadata and a generated code
// (when explicitly requested) are returned.
type TOTPStatus struct {
	OK      bool         `json:"ok"`
	Message string       `json:"message"`
	Entries []totp.Entry `json:"entries,omitempty"`
}

type TOTPCodeResult struct {
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
	Code    totp.Code `json:"code"`
}

func (a *App) GetTOTPEntries() TOTPStatus {
	vault, err := a.getTOTPVault()
	if err != nil {
		return TOTPStatus{OK: false, Message: "本机验证器尚未完成初始化。"}
	}
	entries, err := vault.List()
	if err != nil {
		return TOTPStatus{OK: false, Message: "无法读取本机验证器。请检查系统凭据库权限后重试。"}
	}
	return TOTPStatus{OK: true, Message: "已读取本机验证器。", Entries: entries}
}

func (a *App) AddTOTPEntry(input totp.ImportInput) TOTPStatus {
	vault, err := a.getTOTPVault()
	if err != nil {
		return TOTPStatus{OK: false, Message: "本机验证器尚未完成初始化。"}
	}
	if _, err := vault.Add(input); err != nil {
		return TOTPStatus{OK: false, Message: "无法保存验证器。请检查导入格式与系统凭据库权限后重试。"}
	}
	return a.GetTOTPEntries()
}

func (a *App) GenerateTOTPCode(id string) TOTPCodeResult {
	vault, err := a.getTOTPVault()
	if err != nil {
		return TOTPCodeResult{OK: false, Message: "本机验证器尚未完成初始化。"}
	}
	code, err := vault.Generate(strings.TrimSpace(id), time.Now())
	if err != nil {
		return TOTPCodeResult{OK: false, Message: "无法生成动态验证码。请确认该验证器仍存在且系统凭据库可用。"}
	}
	return TOTPCodeResult{OK: true, Message: "动态验证码已生成。", Code: code}
}

func (a *App) DeleteTOTPEntry(id string) TOTPStatus {
	vault, err := a.getTOTPVault()
	if err != nil {
		return TOTPStatus{OK: false, Message: "本机验证器尚未完成初始化。"}
	}
	if err := vault.Delete(strings.TrimSpace(id)); err != nil {
		return TOTPStatus{OK: false, Message: "无法删除验证器。请检查系统凭据库权限后重试。"}
	}
	return a.GetTOTPEntries()
}

// ExportTOTPEncrypted prompts for a destination only after a user supplies a
// separate export password. It never exports to diagnostics, localStorage, or
// an automatic cloud destination.
func (a *App) ExportTOTPEncrypted(password string) Result {
	if a.ctx == nil {
		return Result{OK: false, Message: "助手尚未完成启动，请稍后再试。"}
	}
	vault, err := a.getTOTPVault()
	if err != nil {
		return Result{OK: false, Message: "本机验证器尚未完成初始化。"}
	}
	data, err := vault.ExportEncrypted(password)
	if err != nil {
		return Result{OK: false, Message: "无法创建加密验证器备份。请确认导出密码与系统凭据库后重试。"}
	}
	destination, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出加密验证器备份",
		DefaultFilename: "XIASS-Tools-TOTP-" + time.Now().Format("20060102-150405") + ".json",
		Filters: []runtime.FileFilter{{
			DisplayName: "XIASS Tools 加密备份 (*.json)", Pattern: "*.json",
		}},
	})
	if err != nil {
		return Result{OK: false, Message: "无法打开备份保存窗口。"}
	}
	if strings.TrimSpace(destination) == "" {
		return Result{OK: true, Message: "已取消导出加密验证器备份。"}
	}
	if !strings.EqualFold(filepath.Ext(destination), ".json") {
		destination += ".json"
	}
	if err := writeNewSensitiveExport(destination, data); err != nil {
		return Result{OK: false, Message: "无法保存加密验证器备份。请确认目标位置可写且文件不存在。"}
	}
	return Result{OK: true, Message: "已导出加密验证器备份。请妥善保管导出密码与文件。"}
}

func (a *App) getTOTPVault() (*totp.Vault, error) {
	if a == nil || a.totpVault == nil {
		return nil, errors.New("系统凭据库尚未完成初始化")
	}
	return a.totpVault, nil
}

// writeNewSensitiveExport refuses to overwrite a file selected by mistake.
// A user can explicitly choose a different name; this avoids destructive
// replacement of an existing encrypted backup.
func writeNewSensitiveExport(destination string, data []byte) error {
	destination = filepath.Clean(strings.TrimSpace(destination))
	if destination == "" || destination == "." {
		return errors.New("导出路径无效")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("目标文件已存在，请选择新的文件名以避免覆盖现有备份")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	success = true
	return nil
}
