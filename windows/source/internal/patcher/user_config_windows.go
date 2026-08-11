//go:build windows

package patcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const windowsCloudCodeSetting = "jetski.cloudCodeUrl"

type windowsProductMetadata struct {
	NameShort      string `json:"nameShort"`
	DataFolderName string `json:"dataFolderName"`
}

type windowsJSONCMember struct {
	key       string
	keyBeg    int
	valueBeg  int
	valueEnd  int
	memberEnd int
	hasComma  bool
}

type windowsJSONCObject struct {
	close   int
	members []windowsJSONCMember
}

// windowsTargetConnectionSupport accepts only the official configuration path
// proven by the installed main process and extension. It never infers support
// from a filename or from an old helper marker.
func windowsTargetConnectionSupport(target windowsTarget) (bool, string, string) {
	if target.kind == "agent" {
		if err := windowsTargetASARConnectionSupport(target); err != nil {
			return false, "", err.Error()
		}
		return true, "asar-minimal", ""
	}
	if _, _, err := windowsTargetUserSettingsPath(target); err != nil {
		return false, "", err.Error()
	}
	return true, "user-settings", ""
}

// windowsTargetASARConnectionSupport accepts only the exact packaged Agent
// structure for which the minimal endpoint adapter has been verified. Unlike
// the IDE path, Agent has no official user-setting chain, so this adapter
// validates the ASAR entry that will be changed before a candidate is built.
func windowsTargetASARConnectionSupport(target windowsTarget) error {
	if target.asar == "" || target.language == "" {
		return fmt.Errorf("该 Antigravity 2.0 安装缺少 app.asar 或 Language Server，无法安全连接")
	}
	if windowsExistingFile(target.asar) == "" || windowsExistingFile(target.language) == "" {
		return fmt.Errorf("该 Antigravity 2.0 安装文件不完整，未修改任何文件")
	}
	source, err := windowsPatchSource(target.asar)
	if err != nil {
		return err
	}
	archive, err := readASAR(source)
	if err != nil {
		return fmt.Errorf("无法读取 app.asar: %w", err)
	}
	if _, err := archive.readFile("dist/main.js"); err != nil {
		return fmt.Errorf("app.asar 缺少已验证的 dist/main.js")
	}
	launcher, err := archive.readFile("dist/languageServer.js")
	if err != nil {
		return fmt.Errorf("app.asar 缺少已验证的 dist/languageServer.js")
	}
	launcherSource := string(launcher)
	if len(windowsCloudCodeFlagPattern.FindAllStringIndex(launcherSource, -1)) != 1 ||
		!windowsLauncherHasProxyEndpoint(patchWindowsCloudCodeSource(launcherSource)) {
		return fmt.Errorf("app.asar Language Server 端点结构尚未验证，未修改任何文件")
	}
	languageSource, err := windowsPatchSource(target.language)
	if err != nil {
		return err
	}
	if _, err := validateWindowsAgentEmbeddedUISource(languageSource); err != nil {
		return fmt.Errorf("Antigravity 2.0 图片界面校验失败: %w", err)
	}
	return nil
}

func windowsTargetUserSettingsPath(target windowsTarget) (string, string, error) {
	if target.kind != "ide" || target.main == "" || target.extensionEntry == "" {
		return "", "", fmt.Errorf("该安装尚未验证官方用户级代理设置接入；为保护完整性，助手不会修改安装目录")
	}
	mainSource, err := os.ReadFile(target.main)
	if err != nil {
		return "", "", fmt.Errorf("读取主进程结构失败: %w", err)
	}
	extensionSource, err := os.ReadFile(target.extensionEntry)
	if err != nil {
		return "", "", fmt.Errorf("读取扩展结构失败: %w", err)
	}
	if !strings.Contains(string(mainSource), windowsCloudCodeSetting) ||
		!strings.Contains(string(extensionSource), windowsCloudCodeSetting) ||
		!strings.Contains(string(extensionSource), "--cloud_code_endpoint") {
		return "", "", fmt.Errorf("未找到官方 %s 配置链路；若此前使用过 v1.4.23，请先用官方安装器覆盖重装后再连接", windowsCloudCodeSetting)
	}
	productPath := filepath.Join(target.root, "resources", "app", "product.json")
	productData, err := os.ReadFile(productPath)
	if err != nil {
		return "", "", fmt.Errorf("读取官方产品信息失败: %w", err)
	}
	var product windowsProductMetadata
	if err := json.Unmarshal(productData, &product); err != nil {
		return "", "", fmt.Errorf("解析官方产品信息失败: %w", err)
	}
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData == "" {
		appData, err = os.UserConfigDir()
		if err != nil {
			return "", "", fmt.Errorf("定位 Windows 用户配置目录失败: %w", err)
		}
	}
	names := uniqueNonEmptyStrings(product.NameShort, product.DataFolderName)
	if len(names) == 0 {
		return "", "", fmt.Errorf("官方产品信息未提供用户配置目录名称")
	}
	for _, name := range names {
		candidate := filepath.Join(appData, name, "User", "settings.json")
		if windowsExistingFile(candidate) != "" {
			return candidate, "user-settings", nil
		}
	}
	return filepath.Join(appData, names[0], "User", "settings.json"), "user-settings", nil
}

func uniqueNonEmptyStrings(values ...string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func windowsCloudCodeSettingIsConfigured(path, endpoint string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	document, err := parseWindowsJSONCObject(string(data))
	if err != nil {
		return false
	}
	for _, member := range document.members {
		if member.key != windowsCloudCodeSetting {
			continue
		}
		value, err := strconv.Unquote(string(data[member.valueBeg:member.valueEnd]))
		return err == nil && value == endpoint
	}
	return false
}

// windowsEnsureCloudCodeSetting adds only the one official endpoint setting.
// Existing values are never replaced, and malformed JSONC is never rewritten.
func windowsEnsureCloudCodeSetting(path, endpoint string) (bool, error) {
	plan, changed, err := prepareWindowsEnsureCloudCodeSetting(path, endpoint)
	if err != nil || !changed {
		return changed, err
	}
	return true, windowsWriteFileAtomic(plan.path, plan.updated, plan.mode)
}

func prepareWindowsEnsureCloudCodeSetting(path, endpoint string) (*windowsPatchPlan, bool, error) {
	data, err := os.ReadFile(path)
	mode := os.FileMode(0o600)
	if os.IsNotExist(err) {
		data = []byte("{\n}\n")
	} else if err != nil {
		return nil, false, err
	} else if info, statErr := os.Stat(path); statErr != nil {
		return nil, false, statErr
	} else {
		mode = info.Mode()
	}
	document, err := parseWindowsJSONCObject(string(data))
	if err != nil {
		return nil, false, fmt.Errorf("%s 不是可安全编辑的 JSONC 设置文件: %w", path, err)
	}
	for _, member := range document.members {
		if member.key != windowsCloudCodeSetting {
			continue
		}
		value, unquoteErr := strconv.Unquote(string(data[member.valueBeg:member.valueEnd]))
		if unquoteErr != nil {
			return nil, false, fmt.Errorf("%s 的 %s 不是字符串，已拒绝覆盖", path, windowsCloudCodeSetting)
		}
		if value == endpoint {
			return nil, false, nil
		}
		updated := append([]byte(nil), data[:member.valueBeg]...)
		updated = append(updated, []byte(strconv.Quote(endpoint))...)
		updated = append(updated, data[member.valueEnd:]...)
		return &windowsPatchPlan{
			path: path, original: data, updated: updated, mode: mode, changed: true,
		}, true, nil
	}
	entry := `  "` + windowsCloudCodeSetting + `": ` + strconv.Quote(endpoint)
	updated := string(data)
	if len(document.members) == 0 {
		updated = updated[:document.close] + "\n" + entry + "\n" + updated[document.close:]
	} else {
		last := document.members[len(document.members)-1]
		close := document.close
		if !last.hasComma {
			updated = updated[:last.valueEnd] + "," + updated[last.valueEnd:]
			close++
		}
		updated = updated[:close] + "\n" + entry + "\n" + updated[close:]
	}
	return &windowsPatchPlan{
		path: path, original: data, updated: []byte(updated), mode: mode, changed: true,
	}, true, nil
}

// prepareWindowsRemoveCloudCodeSetting removes only the endpoint value written
// by this helper. A user-provided endpoint is never changed.
func prepareWindowsRemoveCloudCodeSetting(path, endpoint string) (*windowsPatchPlan, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	document, err := parseWindowsJSONCObject(string(data))
	if err != nil {
		return nil, false, fmt.Errorf("%s 不是可安全编辑的 JSONC 设置文件: %w", path, err)
	}
	for _, member := range document.members {
		if member.key != windowsCloudCodeSetting {
			continue
		}
		value, unquoteErr := strconv.Unquote(string(data[member.valueBeg:member.valueEnd]))
		if unquoteErr != nil || value != endpoint {
			return nil, false, nil
		}
		updated := append([]byte(nil), data[:member.keyBeg]...)
		updated = append(updated, data[member.memberEnd:]...)
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, false, statErr
		}
		return &windowsPatchPlan{
			path: path, original: data, updated: updated, mode: info.Mode(), changed: true,
		}, true, nil
	}
	return nil, false, nil
}

func parseWindowsJSONCObject(source string) (windowsJSONCObject, error) {
	position, err := skipWindowsJSONCTrivia(source, 0)
	if err != nil {
		return windowsJSONCObject{}, err
	}
	if position >= len(source) || source[position] != '{' {
		return windowsJSONCObject{}, fmt.Errorf("根节点不是对象")
	}
	position++
	document := windowsJSONCObject{}
	for {
		position, err = skipWindowsJSONCTrivia(source, position)
		if err != nil {
			return windowsJSONCObject{}, err
		}
		if position >= len(source) {
			return windowsJSONCObject{}, fmt.Errorf("对象未闭合")
		}
		if source[position] == '}' {
			document.close = position
			return document, nil
		}
		if source[position] != '"' {
			return windowsJSONCObject{}, fmt.Errorf("属性名不是字符串")
		}
		keyBeg := position
		keyEnd, key, err := scanWindowsJSONCString(source, position)
		if err != nil {
			return windowsJSONCObject{}, err
		}
		position, err = skipWindowsJSONCTrivia(source, keyEnd)
		if err != nil || position >= len(source) || source[position] != ':' {
			return windowsJSONCObject{}, fmt.Errorf("属性 %q 缺少冒号", key)
		}
		valueBeg, err := skipWindowsJSONCTrivia(source, position+1)
		if err != nil {
			return windowsJSONCObject{}, err
		}
		valueEnd, err := scanWindowsJSONCValue(source, valueBeg)
		if err != nil {
			return windowsJSONCObject{}, err
		}
		position, err = skipWindowsJSONCTrivia(source, valueEnd)
		if err != nil || position >= len(source) {
			return windowsJSONCObject{}, fmt.Errorf("属性 %q 的值未结束", key)
		}
		member := windowsJSONCMember{key: key, keyBeg: keyBeg, valueBeg: valueBeg, valueEnd: valueEnd, memberEnd: valueEnd}
		if source[position] == ',' {
			member.hasComma = true
			position++
			member.memberEnd = position
		} else if source[position] != '}' {
			return windowsJSONCObject{}, fmt.Errorf("属性 %q 后缺少逗号", key)
		}
		document.members = append(document.members, member)
	}
}

func skipWindowsJSONCTrivia(source string, position int) (int, error) {
	for position < len(source) {
		if strings.ContainsRune(" \t\r\n\ufeff", rune(source[position])) {
			position++
			continue
		}
		if source[position] != '/' || position+1 >= len(source) {
			return position, nil
		}
		switch source[position+1] {
		case '/':
			position += 2
			for position < len(source) && source[position] != '\n' {
				position++
			}
		case '*':
			end := strings.Index(source[position+2:], "*/")
			if end < 0 {
				return 0, fmt.Errorf("块注释未闭合")
			}
			position += end + 4
		default:
			return position, nil
		}
	}
	return position, nil
}

func scanWindowsJSONCString(source string, position int) (int, string, error) {
	start := position
	position++
	for position < len(source) {
		switch source[position] {
		case '\\':
			position += 2
		case '"':
			position++
			value, err := strconv.Unquote(source[start:position])
			return position, value, err
		default:
			position++
		}
	}
	return 0, "", fmt.Errorf("字符串未闭合")
}

func scanWindowsJSONCValue(source string, position int) (int, error) {
	if position >= len(source) {
		return 0, fmt.Errorf("缺少值")
	}
	if source[position] == '"' {
		end, _, err := scanWindowsJSONCString(source, position)
		return end, err
	}
	if source[position] != '{' && source[position] != '[' {
		end := position
		for end < len(source) && !strings.ContainsRune(" \t\r\n,}]", rune(source[end])) {
			end++
		}
		if end == position {
			return 0, fmt.Errorf("无效值")
		}
		return end, nil
	}
	stack := []byte{source[position]}
	for position++; position < len(source); position++ {
		if source[position] == '"' {
			end, _, err := scanWindowsJSONCString(source, position)
			if err != nil {
				return 0, err
			}
			position = end - 1
			continue
		}
		if source[position] == '/' {
			next, err := skipWindowsJSONCTrivia(source, position)
			if err != nil {
				return 0, err
			}
			if next != position {
				position = next - 1
				continue
			}
		}
		switch source[position] {
		case '{', '[':
			stack = append(stack, source[position])
		case '}', ']':
			if len(stack) == 0 || (source[position] == '}' && stack[len(stack)-1] != '{') || (source[position] == ']' && stack[len(stack)-1] != '[') {
				return 0, fmt.Errorf("数组或对象闭合不匹配")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return position + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("数组或对象未闭合")
}
