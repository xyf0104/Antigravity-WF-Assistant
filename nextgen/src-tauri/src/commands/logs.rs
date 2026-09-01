use regex::Regex;
use serde::Serialize;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::LazyLock;
use std::time::UNIX_EPOCH;
use uuid::Uuid;
use zip::write::SimpleFileOptions;
use zip::{CompressionMethod, ZipWriter};

use crate::modules::{logger, wf_bridge};

const MAX_DIAGNOSTIC_FILE_BYTES: u64 = 2 << 20;
const MAX_DIAGNOSTIC_FILE_COUNT: usize = 16;
const MAX_DIAGNOSTIC_TOTAL_BYTES: usize = 20 << 20;

static DIAGNOSTIC_SECRET_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r#"(?i)((?:authorization|x-api-key|api[_-]?key|access[_ -]?token|refresh[_ -]?token|id[_ -]?token|csrf[_ -]?token|client[_ -]?secret|password|cookie|set-cookie|credential|oauth[_ -]?code)\s*[\"']?\s*[:=]\s*[\"']?(?:bearer\s+)?)[^\s,;\"']+"#,
    )
    .expect("diagnostic secret regex")
});
static DIAGNOSTIC_URL_SECRET_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"(?i)([?&](?:key|api_key|token|access_token|refresh_token|id_token|csrf_token|code|client_secret)=)[^&\s"']+"#)
        .expect("diagnostic URL secret regex")
});
static DIAGNOSTIC_KNOWN_TOKEN_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\b(?:(?:sk|ghp|github_pat|xox[baprs])[-_A-Za-z0-9]{12,}|AIza[A-Za-z0-9_-]{20,})\b")
        .expect("known token regex")
});
static DIAGNOSTIC_JWT_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\beyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b")
        .expect("JWT regex")
});
static DIAGNOSTIC_EMAIL_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b").expect("email regex")
});
static DIAGNOSTIC_BASE64_REGEX: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"\b[A-Za-z0-9+/]{256,}={0,2}\b").expect("base64 regex"));

#[derive(Debug, Clone, Serialize)]
pub struct ManagedLogFile {
    pub log_file_path: String,
    pub log_file_name: String,
    pub file_size: u64,
    pub modified_at_ms: Option<i64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct LogSnapshot {
    pub log_dir_path: String,
    pub log_file_path: String,
    pub log_file_name: String,
    pub content: String,
    pub line_limit: usize,
    pub file_size: u64,
    pub modified_at_ms: Option<i64>,
    pub available_files: Vec<ManagedLogFile>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DiagnosticExportResult {
    pub path: String,
    pub parent_log_count: usize,
    pub helper_log_count: usize,
    pub helper_status: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct DiagnosticManifest {
    format: u32,
    exported_at: String,
    app_version: String,
    os: String,
    architecture: String,
    data_owners: Vec<DiagnosticDataOwner>,
    exclusions: Vec<&'static str>,
    helper: DiagnosticHelperSummary,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct DiagnosticDataOwner {
    owner: &'static str,
    storage_root_kind: &'static str,
    included: &'static str,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct DiagnosticHelperSummary {
    status: &'static str,
    storage_root_kind: &'static str,
    account_store_readable: bool,
    account_count: usize,
    model_count: usize,
    proxy_listening: bool,
}

struct RemoveTemporaryArchive(PathBuf);

impl Drop for RemoveTemporaryArchive {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

fn to_unix_millis(time: std::time::SystemTime) -> Option<i64> {
    time.duration_since(UNIX_EPOCH)
        .ok()
        .map(|duration| duration.as_millis())
        .and_then(|value| i64::try_from(value).ok())
}

fn open_directory(path: &Path) -> Result<(), String> {
    #[cfg(target_os = "macos")]
    {
        std::process::Command::new("open")
            .arg(path)
            .spawn()
            .map_err(|e| format!("打开目录失败: {}", e))?;
    }

    #[cfg(target_os = "windows")]
    {
        std::process::Command::new("explorer")
            .arg(path)
            .spawn()
            .map_err(|e| format!("打开目录失败: {}", e))?;
    }

    #[cfg(target_os = "linux")]
    {
        std::process::Command::new("xdg-open")
            .arg(path)
            .spawn()
            .map_err(|e| format!("打开目录失败: {}", e))?;
    }

    Ok(())
}

fn build_managed_log_file(path: &Path) -> Result<ManagedLogFile, String> {
    let metadata = fs::metadata(path).map_err(|e| format!("读取日志文件元数据失败: {}", e))?;

    Ok(ManagedLogFile {
        log_file_path: path.to_string_lossy().to_string(),
        log_file_name: path
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or_default()
            .to_string(),
        file_size: metadata.len(),
        modified_at_ms: metadata.modified().ok().and_then(to_unix_millis),
    })
}

fn build_available_log_files(paths: Vec<PathBuf>) -> Result<Vec<ManagedLogFile>, String> {
    paths
        .into_iter()
        .map(|path| build_managed_log_file(path.as_path()))
        .collect()
}

#[tauri::command]
pub fn logs_get_snapshot(
    file_name: Option<String>,
    line_limit: Option<usize>,
) -> Result<LogSnapshot, String> {
    let line_limit = logger::clamp_log_tail_lines(line_limit);
    let log_dir = logger::get_log_dir()?;
    let log_file = logger::resolve_managed_log_file(file_name.as_deref())?;
    let content = logger::read_log_tail_lines(&log_file, line_limit)?;
    let metadata = fs::metadata(&log_file).map_err(|e| format!("读取日志文件元数据失败: {}", e))?;
    let available_files = build_available_log_files(logger::list_managed_log_files()?)?;

    Ok(LogSnapshot {
        log_dir_path: log_dir.to_string_lossy().to_string(),
        log_file_path: log_file.to_string_lossy().to_string(),
        log_file_name: log_file
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or_default()
            .to_string(),
        content,
        line_limit,
        file_size: metadata.len(),
        modified_at_ms: metadata.modified().ok().and_then(to_unix_millis),
        available_files,
    })
}

#[tauri::command]
pub fn logs_open_log_directory() -> Result<(), String> {
    let log_dir = logger::get_log_dir()?;
    open_directory(&log_dir)
}

fn redact_diagnostic_text(value: &str) -> String {
    let mut redacted = DIAGNOSTIC_SECRET_REGEX
        .replace_all(value, "${1}[REDACTED]")
        .to_string();
    redacted = DIAGNOSTIC_URL_SECRET_REGEX
        .replace_all(&redacted, "${1}[REDACTED]")
        .to_string();
    redacted = DIAGNOSTIC_KNOWN_TOKEN_REGEX
        .replace_all(&redacted, "[REDACTED]")
        .to_string();
    redacted = DIAGNOSTIC_JWT_REGEX
        .replace_all(&redacted, "[REDACTED]")
        .to_string();
    redacted = DIAGNOSTIC_EMAIL_REGEX
        .replace_all(&redacted, "[REDACTED_EMAIL]")
        .to_string();
    redacted = DIAGNOSTIC_BASE64_REGEX
        .replace_all(&redacted, "[REDACTED_BINARY]")
        .to_string();
    if let Some(home) = dirs::home_dir().and_then(|path| path.to_str().map(str::to_string)) {
        if !home.is_empty() {
            redacted = redacted.replace(&home, "~");
            redacted = redacted.replace(&home.replace('\\', "\\\\"), "~");
        }
    }
    redacted
}

fn read_safe_log_tail(path: &Path) -> Result<Vec<u8>, String> {
    let metadata = fs::symlink_metadata(path).map_err(|_| "无法读取诊断日志元数据".to_string())?;
    if metadata.file_type().is_symlink() || !metadata.is_file() {
        return Err("诊断日志不是安全的常规文件".to_string());
    }
    let mut file = File::open(path).map_err(|_| "无法打开诊断日志".to_string())?;
    let opened = file
        .metadata()
        .map_err(|_| "无法验证诊断日志".to_string())?;
    if !opened.is_file() {
        return Err("诊断日志不是安全的常规文件".to_string());
    }
    let truncated = opened.len() > MAX_DIAGNOSTIC_FILE_BYTES;
    let marker = b"[earlier content truncated]\n";
    let read_limit = if truncated {
        MAX_DIAGNOSTIC_FILE_BYTES.saturating_sub(marker.len() as u64)
    } else {
        opened.len()
    };
    if truncated {
        file.seek(SeekFrom::End(-(read_limit as i64)))
            .map_err(|_| "无法定位诊断日志".to_string())?;
    }
    let mut data = Vec::with_capacity(read_limit as usize + marker.len());
    if truncated {
        data.extend_from_slice(marker);
    }
    file.take(read_limit)
        .read_to_end(&mut data)
        .map_err(|_| "无法读取诊断日志".to_string())?;
    let text = String::from_utf8_lossy(&data);
    Ok(redact_diagnostic_text(&text).into_bytes())
}

fn add_zip_entry(writer: &mut ZipWriter<File>, name: &str, data: &[u8]) -> Result<(), String> {
    if name.contains("..") || name.starts_with('/') || name.starts_with('\\') {
        return Err("诊断归档条目名称无效".to_string());
    }
    let options = SimpleFileOptions::default()
        .compression_method(CompressionMethod::Deflated)
        .unix_permissions(0o600);
    writer
        .start_file(name.replace('\\', "/"), options)
        .map_err(|_| "无法创建诊断归档条目".to_string())?;
    writer
        .write_all(data)
        .map_err(|_| "无法写入诊断归档条目".to_string())
}

fn commit_archive(temporary: &Path, destination: &Path) -> Result<(), String> {
    let backup = destination.with_extension(format!("zip.previous-{}", Uuid::new_v4()));
    let had_destination = match fs::symlink_metadata(destination) {
        Ok(metadata) => {
            if metadata.file_type().is_symlink() || !metadata.is_file() {
                return Err("诊断日志目标不是安全的常规文件".to_string());
            }
            fs::rename(destination, &backup).map_err(|_| "无法暂存原诊断归档".to_string())?;
            true
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => false,
        Err(_) => return Err("无法检查诊断日志目标".to_string()),
    };
    if let Err(error) = fs::rename(temporary, destination) {
        if had_destination {
            let _ = fs::rename(&backup, destination);
        }
        return Err(format!("无法保存诊断日志: {error}"));
    }
    if had_destination {
        let _ = fs::remove_file(backup);
    }
    Ok(())
}

fn write_diagnostic_archive(
    destination: &Path,
    parent_logs: &[PathBuf],
    helper: Result<wf_bridge::WfHelperDiagnosticSnapshot, String>,
) -> Result<DiagnosticExportResult, String> {
    let parent = destination
        .parent()
        .filter(|path| path.is_dir())
        .ok_or_else(|| "诊断日志目标目录不存在".to_string())?;
    let temporary = parent.join(format!(".xiass-diagnostics-{}.tmp", Uuid::new_v4()));
    let _temporary_cleanup = RemoveTemporaryArchive(temporary.clone());
    let file = OpenOptions::new()
        .create_new(true)
        .write(true)
        .open(&temporary)
        .map_err(|_| "无法创建临时诊断归档".to_string())?;
    let mut writer = ZipWriter::new(file);
    let helper_summary = match &helper {
        Ok(snapshot) => DiagnosticHelperSummary {
            status: "available",
            storage_root_kind: "separate_user_home_directory",
            account_store_readable: snapshot.account_store_readable,
            account_count: snapshot.account_count,
            model_count: snapshot.model_count,
            proxy_listening: snapshot.proxy_listening,
        },
        Err(_) => DiagnosticHelperSummary {
            status: "unavailable",
            storage_root_kind: "separate_user_home_directory",
            account_store_readable: false,
            account_count: 0,
            model_count: 0,
            proxy_listening: false,
        },
    };
    let manifest = DiagnosticManifest {
        format: 1,
        exported_at: chrono::Utc::now().to_rfc3339(),
        app_version: env!("CARGO_PKG_VERSION").to_string(),
        os: std::env::consts::OS.to_string(),
        architecture: std::env::consts::ARCH.to_string(),
        data_owners: vec![
            DiagnosticDataOwner {
                owner: "tauri_parent",
                storage_root_kind: "xiass_tools_parent_data_directory",
                included: "bounded_redacted_managed_logs",
            },
            DiagnosticDataOwner {
                owner: "embedded_wf_helper",
                storage_root_kind: "separate_user_home_directory",
                included: "bounded_redacted_helper_logs_and_inventory",
            },
            DiagnosticDataOwner {
                owner: "external_codex_home",
                storage_root_kind: "external_application_directory",
                included: "never",
            },
        ],
        exclusions: vec![
            "accounts/*/account.json",
            "upstream_accounts.json",
            "custom_models.json",
            "provider_and_model_configuration",
            "oauth_tokens_and_cookies",
            "external_codex_auth",
            "auth.json",
            "chat_and_session_history",
            "image_and_binary_payloads",
            "raw_home_and_storage_paths",
        ],
        helper: helper_summary,
    };
    let manifest_data =
        serde_json::to_vec_pretty(&manifest).map_err(|_| "无法生成诊断摘要".to_string())?;
    add_zip_entry(&mut writer, "diagnostic-manifest.json", &manifest_data)?;

    let mut parent_count = 0usize;
    let mut helper_count = 0usize;
    let mut total = manifest_data.len();
    for path in parent_logs.iter().take(MAX_DIAGNOSTIC_FILE_COUNT) {
        let data = read_safe_log_tail(path)?;
        if total + data.len() > MAX_DIAGNOSTIC_TOTAL_BYTES {
            break;
        }
        let name = path
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or("managed.log");
        add_zip_entry(&mut writer, &format!("parent/{name}"), &data)?;
        total += data.len();
        parent_count += 1;
    }
    if let Ok(snapshot) = helper {
        for log in snapshot.logs {
            let data = redact_diagnostic_text(&log.content).into_bytes();
            if data.len() > MAX_DIAGNOSTIC_FILE_BYTES as usize
                || total + data.len() > MAX_DIAGNOSTIC_TOTAL_BYTES
            {
                continue;
            }
            add_zip_entry(&mut writer, &log.name, &data)?;
            total += data.len();
            helper_count += 1;
        }
    }
    let completed = writer
        .finish()
        .map_err(|_| "无法完成诊断归档".to_string())?;
    completed
        .sync_all()
        .map_err(|_| "无法同步诊断归档".to_string())?;
    drop(completed);
    if let Err(error) = commit_archive(&temporary, destination) {
        let _ = fs::remove_file(&temporary);
        return Err(error);
    }
    Ok(DiagnosticExportResult {
        path: destination.to_string_lossy().to_string(),
        parent_log_count: parent_count,
        helper_log_count: helper_count,
        helper_status: if helper_count > 0 {
            "included"
        } else {
            "inventory_only_or_unavailable"
        }
        .to_string(),
    })
}

#[tauri::command]
pub async fn logs_export_diagnostics(
    destination: String,
) -> Result<DiagnosticExportResult, String> {
    let mut path = PathBuf::from(destination.trim());
    if destination.trim().is_empty() {
        return Err("诊断日志保存位置为空".to_string());
    }
    if path
        .extension()
        .and_then(|value| value.to_str())
        .map(|value| value.eq_ignore_ascii_case("zip"))
        != Some(true)
    {
        path.set_extension("zip");
    }
    let parent_logs = logger::list_managed_log_files()?;
    let helper = tauri::async_runtime::spawn_blocking(wf_bridge::get_helper_diagnostics)
        .await
        .map_err(|_| "收集 WF 诊断任务失败".to_string())?;
    tauri::async_runtime::spawn_blocking(move || {
        write_diagnostic_archive(&path, &parent_logs, helper)
    })
    .await
    .map_err(|_| "导出诊断日志任务失败".to_string())?
}

#[cfg(test)]
mod diagnostic_export_tests {
    use super::*;
    use std::io::Read;
    use zip::ZipArchive;

    fn archive_entries(path: &Path) -> Vec<(String, String)> {
        let file = File::open(path).unwrap();
        let mut archive = ZipArchive::new(file).unwrap();
        let mut result = Vec::new();
        for index in 0..archive.len() {
            let mut entry = archive.by_index(index).unwrap();
            let name = entry.name().to_string();
            let mut content = String::new();
            entry.read_to_string(&mut content).unwrap();
            result.push((name, content));
        }
        result
    }

    #[test]
    fn diagnostic_archive_redacts_and_excludes_owned_credentials() {
        let root = std::env::temp_dir().join(format!("xiass-diagnostic-test-{}", Uuid::new_v4()));
        fs::create_dir_all(&root).unwrap();
        let log = root.join("app.log");
        fs::write(&log, "email=user@example.com Authorization: Bearer private-token api_key=sk-secret1234567890").unwrap();
        fs::write(root.join("accounts.json"), "must-never-appear").unwrap();
        let destination = root.join("diagnostics.zip");
        let helper = wf_bridge::WfHelperDiagnosticSnapshot {
            schema_version: 1,
            storage_owner: "embedded_wf_helper".to_string(),
            storage_root_kind: "separate_user_home_directory".to_string(),
            account_store_readable: true,
            account_count: 2,
            model_count: 3,
            proxy_listening: true,
            logs: vec![wf_bridge::WfHelperDiagnosticLog {
                name: "wf-helper/xiass-tools.log".to_string(),
                content: "cookie=helper-secret normal-event".to_string(),
            }],
            exclusions: vec!["external_codex_auth".to_string()],
        };
        write_diagnostic_archive(&destination, &[log], Ok(helper)).unwrap();
        let combined = archive_entries(&destination)
            .into_iter()
            .map(|(name, value)| name + &value)
            .collect::<String>();
        for forbidden in [
            "user@example.com",
            "private-token",
            "sk-secret1234567890",
            "helper-secret",
            "must-never-appear",
        ] {
            assert!(!combined.contains(forbidden), "leaked {forbidden}");
        }
        assert!(combined.contains("external_codex_auth"));
        assert!(combined.contains("[REDACTED]"));
        let _ = fs::remove_dir_all(root);
    }

    #[cfg(unix)]
    #[test]
    fn diagnostic_archive_rejects_symlinked_log() {
        let root =
            std::env::temp_dir().join(format!("xiass-diagnostic-link-test-{}", Uuid::new_v4()));
        fs::create_dir_all(&root).unwrap();
        let outside = root.join("outside.log");
        fs::write(&outside, "outside-secret").unwrap();
        let linked = root.join("app.log");
        std::os::unix::fs::symlink(&outside, &linked).unwrap();
        let result = write_diagnostic_archive(
            &root.join("diagnostics.zip"),
            &[linked],
            Err("unavailable".to_string()),
        );
        assert!(result.is_err());
        assert!(!root.join("diagnostics.zip").exists());
        let _ = fs::remove_dir_all(root);
    }
}
