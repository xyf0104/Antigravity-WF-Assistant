use std::collections::{HashMap, HashSet};
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Arc, LazyLock, Mutex};

use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::models::codex::{
    CodexConfigBackupInfo, CodexConfigBackupVerification, CodexConfigRestoreResult,
};

const BACKUP_DIRECTORY_NAME: &str = ".xiass-tools-codex-config-backups";
const SNAPSHOT_FILE_NAME: &str = "config.toml";
const MANIFEST_FILE_NAME: &str = "manifest.json";
const MANIFEST_VERSION: u32 = 1;
const DEFAULT_SOURCE: &str = "nextgen-codex-config";
const MAX_COMPLETED_BACKUPS: usize = 24;
// A malformed current config must never be promoted to a user-visible recovery
// point. It is still retained privately so a failed restore can put the exact
// original bytes back without attempting to parse or normalize them.
const PRIVATE_RESTORE_SAFETY_SNAPSHOT_KIND: &str = "private-restore-safety-v1";

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct PrivateRestoreSafetySnapshot {
    version: u32,
    id: String,
    created_at: i64,
    original_existed: bool,
    bytes: u64,
    sha256: String,
    kind: String,
}

enum RestoreSafetyBackup {
    Verified(CodexConfigBackupInfo),
    Private(PrivateRestoreSafetySnapshot),
}

#[cfg(target_os = "windows")]
const FILE_ATTRIBUTE_REPARSE_POINT: u32 = 0x0000_0400;

static CONFIG_TRANSACTION_LOCKS: LazyLock<Mutex<HashMap<PathBuf, Arc<Mutex<()>>>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

#[cfg(test)]
thread_local! {
    // This only lets the focused transaction test emulate an I/O error after
    // replacement has committed. It is thread-local so parallel tests cannot
    // observe another test's fault injection.
    static TEST_FAIL_NEXT_CONFIG_WRITE_AFTER_REPLACE: std::cell::Cell<bool> = std::cell::Cell::new(false);
}

#[cfg(test)]
fn fail_next_config_write_after_replace_for_test() {
    TEST_FAIL_NEXT_CONFIG_WRITE_AFTER_REPLACE.with(|flag| flag.set(true));
}

#[cfg(test)]
fn take_test_config_write_failure_after_replace() -> bool {
    TEST_FAIL_NEXT_CONFIG_WRITE_AFTER_REPLACE.with(|flag| flag.replace(false))
}

fn transaction_lock(path: &Path) -> Result<Arc<Mutex<()>>, String> {
    let mut locks = CONFIG_TRANSACTION_LOCKS
        .lock()
        .map_err(|_| "Codex 配置事务锁已损坏".to_string())?;
    Ok(locks
        .entry(path.to_path_buf())
        .or_insert_with(|| Arc::new(Mutex::new(())))
        .clone())
}

#[cfg(target_os = "windows")]
fn is_unsafe_link(metadata: &fs::Metadata) -> bool {
    use std::os::windows::fs::MetadataExt;

    metadata.file_type().is_symlink()
        || metadata.file_attributes() & FILE_ATTRIBUTE_REPARSE_POINT != 0
}

#[cfg(not(target_os = "windows"))]
fn is_unsafe_link(metadata: &fs::Metadata) -> bool {
    metadata.file_type().is_symlink()
}

#[cfg(unix)]
fn set_private_directory_permissions(path: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;

    fs::set_permissions(path, fs::Permissions::from_mode(0o700))
        .map_err(|error| format!("设置 Codex 配置备份目录权限失败: {}", error))
}

#[cfg(not(unix))]
fn set_private_directory_permissions(_path: &Path) -> Result<(), String> {
    Ok(())
}

#[cfg(unix)]
fn set_private_file_permissions(path: &Path) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;

    fs::set_permissions(path, fs::Permissions::from_mode(0o600))
        .map_err(|error| format!("设置 Codex 配置备份文件权限失败: {}", error))
}

#[cfg(not(unix))]
fn set_private_file_permissions(_path: &Path) -> Result<(), String> {
    Ok(())
}

fn ensure_existing_directory(path: &Path, label: &str) -> Result<(), String> {
    let metadata = fs::symlink_metadata(path).map_err(|_| format!("无法读取 {}", label))?;
    if !metadata.is_dir() || is_unsafe_link(&metadata) {
        return Err(format!("{} 不是受信任的目录", label));
    }
    Ok(())
}

fn ensure_existing_private_directory(path: &Path, label: &str) -> Result<(), String> {
    ensure_existing_directory(path, label)?;
    set_private_directory_permissions(path)
}

fn ensure_private_directory(path: &Path, label: &str) -> Result<(), String> {
    match fs::symlink_metadata(path) {
        Ok(_) => {}
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => match fs::create_dir(path) {
            Ok(()) => {}
            Err(create_error) if create_error.kind() == std::io::ErrorKind::AlreadyExists => {}
            Err(_) => return Err(format!("无法创建 {}", label)),
        },
        Err(_) => return Err(format!("无法读取 {}", label)),
    }
    ensure_existing_private_directory(path, label)
}

fn ensure_existing_private_file(path: &Path, label: &str) -> Result<(), String> {
    let metadata = fs::symlink_metadata(path).map_err(|_| format!("无法读取 {}", label))?;
    if !metadata.is_file() || is_unsafe_link(&metadata) {
        return Err(format!("{} 不是受信任的普通文件", label));
    }
    set_private_file_permissions(path)
}

fn ensure_regular_config_file_or_absent(path: &Path) -> Result<(), String> {
    match fs::symlink_metadata(path) {
        Ok(metadata) if metadata.is_file() && !is_unsafe_link(&metadata) => Ok(()),
        Ok(_) => Err("Codex 配置文件不是受信任的普通文件".to_string()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(_) => Err("无法读取 Codex 配置文件".to_string()),
    }
}

fn config_parent(path: &Path, create_if_missing: bool) -> Result<PathBuf, String> {
    let parent = path.parent().ok_or("无法定位 Codex 配置目录")?;
    // Check the parent path before canonicalization. Canonicalizing first would
    // silently accept a user-provided `.codex` symlink or Windows junction.
    match fs::symlink_metadata(parent) {
        Ok(_) => ensure_existing_directory(parent, "Codex 配置目录")?,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound && create_if_missing => {
            fs::create_dir_all(parent).map_err(|_| "无法创建 Codex 配置目录".to_string())?;
            ensure_existing_directory(parent, "Codex 配置目录")?;
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            return Err("无法定位 Codex 配置目录".to_string())
        }
        Err(_) => return Err("无法读取 Codex 配置目录".to_string()),
    }
    let canonical_parent =
        fs::canonicalize(parent).map_err(|_| "无法定位 Codex 配置目录".to_string())?;
    ensure_existing_directory(&canonical_parent, "Codex 配置目录")?;
    Ok(canonical_parent)
}

fn resolve_config_target(path: &Path, create_parent_if_missing: bool) -> Result<PathBuf, String> {
    let file_name = path.file_name().ok_or("无法解析 Codex 配置文件名")?;
    let canonical_parent = config_parent(path, create_parent_if_missing)?;
    ensure_regular_config_file_or_absent(path)?;
    let resolved = canonical_parent.join(file_name);
    ensure_regular_config_file_or_absent(&resolved)?;
    Ok(resolved)
}

fn resolve_existing_config_target(path: &Path) -> Result<Option<PathBuf>, String> {
    let parent = path.parent().ok_or("无法定位 Codex 配置目录")?;
    match fs::symlink_metadata(parent) {
        Ok(_) => resolve_config_target(path, false).map(Some),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(_) => Err("无法读取 Codex 配置目录".to_string()),
    }
}

fn backup_root(path: &Path) -> Result<PathBuf, String> {
    Ok(config_parent(path, false)?.join(BACKUP_DIRECTORY_NAME))
}

fn ensure_backup_root(path: &Path) -> Result<PathBuf, String> {
    let root = config_parent(path, true)?.join(BACKUP_DIRECTORY_NAME);
    ensure_private_directory(&root, "Codex 配置备份目录")?;
    Ok(root)
}

fn existing_backup_root(path: &Path) -> Result<Option<PathBuf>, String> {
    let parent = path.parent().ok_or("无法定位 Codex 配置目录")?;
    if !parent.exists() {
        return Ok(None);
    }
    let root = backup_root(path)?;
    match fs::symlink_metadata(&root) {
        Ok(_) => Ok(Some(root)),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(_) => Err("无法读取 Codex 配置备份目录".to_string()),
    }
}

fn backup_directory(path: &Path, backup_id: &str) -> Result<PathBuf, String> {
    let id = parse_backup_id(backup_id)?;
    Ok(backup_root(path)?.join(id))
}

fn parse_backup_id(value: &str) -> Result<String, String> {
    Uuid::parse_str(value.trim())
        .map(|id| id.to_string())
        .map_err(|_| "Codex 配置备份 ID 无效".to_string())
}

fn sha256_hex(content: &[u8]) -> String {
    format!("{:x}", Sha256::digest(content))
}

fn now_unix_millis() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
        .try_into()
        .unwrap_or(i64::MAX)
}

fn normalize_source(source: &str) -> String {
    let candidate = source.trim();
    if candidate.is_empty()
        || candidate.len() > 96
        || !candidate
            .chars()
            .all(|ch| ch.is_ascii_alphanumeric() || matches!(ch, '-' | '_' | '.' | '/'))
    {
        return DEFAULT_SOURCE.to_string();
    }
    candidate.to_string()
}

fn open_private_new_file(path: &Path, label: &str) -> Result<fs::File, String> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    options
        .open(path)
        .map_err(|_| format!("无法创建 {}", label))
}

#[cfg(unix)]
fn sync_parent_directory(path: &Path) -> Result<(), String> {
    let parent = path.parent().ok_or("无法定位目标目录")?;
    fs::File::open(parent)
        .and_then(|directory| directory.sync_all())
        .map_err(|_| "无法同步 Codex 配置目录".to_string())
}

#[cfg(not(unix))]
fn sync_parent_directory(_path: &Path) -> Result<(), String> {
    Ok(())
}

// A snapshot is created inside a fresh UUID directory. `create_new` and a
// successful fsync happen before its manifest is written, so an interrupted
// operation can never look like a restorable backup.
fn write_private_new_file(path: &Path, content: &[u8], label: &str) -> Result<(), String> {
    let mut file = open_private_new_file(path, label)?;
    file.write_all(content)
        .and_then(|_| file.sync_all())
        .map_err(|_| format!("无法写入 {}", label))?;
    drop(file);
    set_private_file_permissions(path)?;
    sync_parent_directory(path)
}

fn read_private_file(path: &Path, label: &str) -> Result<Vec<u8>, String> {
    ensure_existing_private_file(path, label)?;
    fs::read(path).map_err(|_| format!("无法读取 {}", label))
}

fn read_manifest(directory: &Path) -> Result<CodexConfigBackupInfo, String> {
    let manifest_path = directory.join(MANIFEST_FILE_NAME);
    let content = read_private_file(&manifest_path, "Codex 配置备份清单")?;
    let manifest: CodexConfigBackupInfo =
        serde_json::from_slice(&content).map_err(|_| "Codex 配置备份清单无效".to_string())?;
    if !manifest.valid {
        return Err("Codex 配置备份清单状态无效".to_string());
    }
    if manifest.version != MANIFEST_VERSION || parse_backup_id(&manifest.id).is_err() {
        return Err("Codex 配置备份清单版本或 ID 无效".to_string());
    }
    if manifest.sha256.len() != 64 || !manifest.sha256.chars().all(|ch| ch.is_ascii_hexdigit()) {
        return Err("Codex 配置备份清单校验值无效".to_string());
    }
    Ok(manifest)
}

fn validate_snapshot(manifest: &CodexConfigBackupInfo, snapshot: &[u8]) -> Result<(), String> {
    if manifest.bytes != snapshot.len() as u64
        || !sha256_hex(snapshot).eq_ignore_ascii_case(&manifest.sha256)
    {
        return Err("Codex 配置备份校验失败".to_string());
    }
    if !manifest.original_existed {
        if snapshot.is_empty() {
            return Ok(());
        }
        return Err("Codex 配置备份状态无效".to_string());
    }
    let content =
        std::str::from_utf8(snapshot).map_err(|_| "Codex 配置备份不是 UTF-8 文本".to_string())?;
    crate::modules::codex_config_format::read_codex_config_doc_from_str(content)
        .map_err(|_| "Codex 配置备份 TOML 校验失败".to_string())?;
    Ok(())
}

fn read_verified_snapshot(
    path: &Path,
    backup_id: &str,
) -> Result<(CodexConfigBackupInfo, Vec<u8>), String> {
    let root = backup_root(path)?;
    ensure_existing_private_directory(&root, "Codex 配置备份目录")?;
    let directory = backup_directory(path, backup_id)?;
    ensure_existing_private_directory(&directory, "Codex 配置备份")?;
    let manifest = read_manifest(&directory)?;
    if manifest.id != parse_backup_id(backup_id)? {
        return Err("Codex 配置备份清单与请求 ID 不匹配".to_string());
    }
    let snapshot = read_private_file(&directory.join(SNAPSHOT_FILE_NAME), "Codex 配置备份快照")?;
    validate_snapshot(&manifest, &snapshot)?;
    Ok((manifest, snapshot))
}

fn read_current_config_snapshot(path: &Path) -> Result<(bool, Vec<u8>), String> {
    ensure_regular_config_file_or_absent(path)?;
    match fs::read(path) {
        Ok(content) => Ok((true, content)),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok((false, Vec::new())),
        Err(_) => Err("无法读取当前 Codex 配置，未执行写入".to_string()),
    }
}

fn create_private_backup_directory(root: &Path, backup_id: &str) -> Result<PathBuf, String> {
    let id = parse_backup_id(backup_id)?;
    ensure_existing_private_directory(root, "Codex 配置备份目录")?;
    let directory = root.join(id);
    match fs::create_dir(&directory) {
        Ok(()) => {}
        Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
            return Err("Codex 配置备份 ID 冲突，未执行写入".to_string())
        }
        Err(_) => return Err("无法创建 Codex 配置备份，未执行写入".to_string()),
    }
    ensure_existing_private_directory(&directory, "Codex 配置备份")?;
    Ok(directory)
}

fn validate_current_config_snapshot(original_existed: bool, snapshot: &[u8]) -> Result<(), String> {
    if original_existed {
        let content = std::str::from_utf8(snapshot)
            .map_err(|_| "当前 Codex 配置不是 UTF-8 文本，未执行写入".to_string())?;
        crate::modules::codex_config_format::read_codex_config_doc_from_str(content)
            .map_err(|_| "当前 Codex 配置未通过 TOML 校验，未执行写入".to_string())?;
    }
    Ok(())
}

fn create_verified_backup_from_snapshot_locked(
    path: &Path,
    source: &str,
    original_existed: bool,
    snapshot: &[u8],
) -> Result<CodexConfigBackupInfo, String> {
    validate_current_config_snapshot(original_existed, snapshot)?;

    let backup_id = Uuid::new_v4().to_string();
    let root = ensure_backup_root(path)?;
    let directory = create_private_backup_directory(&root, &backup_id)?;
    let manifest = CodexConfigBackupInfo {
        version: MANIFEST_VERSION,
        id: backup_id,
        created_at: now_unix_millis(),
        source: normalize_source(source),
        original_existed,
        bytes: snapshot.len() as u64,
        sha256: sha256_hex(snapshot),
        valid: true,
    };

    write_private_new_file(
        &directory.join(SNAPSHOT_FILE_NAME),
        &snapshot,
        "Codex 配置备份快照",
    )?;
    let manifest_json = serde_json::to_vec_pretty(&manifest)
        .map_err(|_| "无法生成 Codex 配置备份清单，未执行写入".to_string())?;
    write_private_new_file(
        &directory.join(MANIFEST_FILE_NAME),
        &manifest_json,
        "Codex 配置备份清单",
    )?;

    let (verified_manifest, verified_snapshot) = read_verified_snapshot(path, &manifest.id)?;
    if verified_manifest.sha256 != manifest.sha256 || verified_snapshot != snapshot {
        return Err("Codex 配置备份复核失败，未执行写入".to_string());
    }
    Ok(manifest)
}

fn create_backup_locked(path: &Path, source: &str) -> Result<CodexConfigBackupInfo, String> {
    let (original_existed, snapshot) = read_current_config_snapshot(path)?;
    create_verified_backup_from_snapshot_locked(path, source, original_existed, &snapshot)
}

fn validate_private_restore_safety_snapshot(
    manifest: &PrivateRestoreSafetySnapshot,
    snapshot: &[u8],
) -> Result<(), String> {
    if manifest.version != MANIFEST_VERSION
        || parse_backup_id(&manifest.id).is_err()
        || manifest.kind != PRIVATE_RESTORE_SAFETY_SNAPSHOT_KIND
    {
        return Err("Codex 私有安全快照清单无效".to_string());
    }
    if manifest.sha256.len() != 64 || !manifest.sha256.chars().all(|ch| ch.is_ascii_hexdigit()) {
        return Err("Codex 私有安全快照校验值无效".to_string());
    }
    if manifest.bytes != snapshot.len() as u64
        || !sha256_hex(snapshot).eq_ignore_ascii_case(&manifest.sha256)
    {
        return Err("Codex 私有安全快照校验失败".to_string());
    }
    if !manifest.original_existed && !snapshot.is_empty() {
        return Err("Codex 私有安全快照状态无效".to_string());
    }
    Ok(())
}

fn read_private_restore_safety_snapshot(
    path: &Path,
    backup_id: &str,
) -> Result<(PrivateRestoreSafetySnapshot, Vec<u8>), String> {
    let root = backup_root(path)?;
    ensure_existing_private_directory(&root, "Codex 配置备份目录")?;
    let directory = backup_directory(path, backup_id)?;
    ensure_existing_private_directory(&directory, "Codex 私有安全快照")?;
    let manifest_path = directory.join(MANIFEST_FILE_NAME);
    let manifest_content = read_private_file(&manifest_path, "Codex 私有安全快照清单")?;
    let manifest: PrivateRestoreSafetySnapshot = serde_json::from_slice(&manifest_content)
        .map_err(|_| "Codex 私有安全快照清单无效".to_string())?;
    if manifest.id != parse_backup_id(backup_id)? {
        return Err("Codex 私有安全快照清单与请求 ID 不匹配".to_string());
    }
    let snapshot = read_private_file(&directory.join(SNAPSHOT_FILE_NAME), "Codex 私有安全快照")?;
    validate_private_restore_safety_snapshot(&manifest, &snapshot)?;
    Ok((manifest, snapshot))
}

fn create_private_restore_safety_snapshot_locked(
    path: &Path,
    original_existed: bool,
    snapshot: &[u8],
) -> Result<PrivateRestoreSafetySnapshot, String> {
    let backup_id = Uuid::new_v4().to_string();
    let root = ensure_backup_root(path)?;
    let directory = create_private_backup_directory(&root, &backup_id)?;
    let manifest = PrivateRestoreSafetySnapshot {
        version: MANIFEST_VERSION,
        id: backup_id,
        created_at: now_unix_millis(),
        original_existed,
        bytes: snapshot.len() as u64,
        sha256: sha256_hex(snapshot),
        kind: PRIVATE_RESTORE_SAFETY_SNAPSHOT_KIND.to_string(),
    };

    write_private_new_file(
        &directory.join(SNAPSHOT_FILE_NAME),
        snapshot,
        "Codex 私有安全快照",
    )?;
    let manifest_json = serde_json::to_vec_pretty(&manifest)
        .map_err(|_| "无法生成 Codex 私有安全快照清单，未执行恢复".to_string())?;
    write_private_new_file(
        &directory.join(MANIFEST_FILE_NAME),
        &manifest_json,
        "Codex 私有安全快照清单",
    )?;

    let (verified_manifest, verified_snapshot) =
        read_private_restore_safety_snapshot(path, &manifest.id)?;
    if verified_manifest.id != manifest.id || verified_snapshot != snapshot {
        return Err("Codex 私有安全快照复核失败，未执行恢复".to_string());
    }
    Ok(manifest)
}

fn create_restore_safety_backup_locked(path: &Path) -> Result<RestoreSafetyBackup, String> {
    let (original_existed, snapshot) = read_current_config_snapshot(path)?;
    match validate_current_config_snapshot(original_existed, &snapshot) {
        Ok(()) => create_verified_backup_from_snapshot_locked(
            path,
            "nextgen-codex-config-restore",
            original_existed,
            &snapshot,
        )
        .map(RestoreSafetyBackup::Verified),
        Err(_) => create_private_restore_safety_snapshot_locked(path, original_existed, &snapshot)
            .map(RestoreSafetyBackup::Private),
    }
}

// The general atomic writer creates a legacy `.bak` file with its own policy.
// Config transactions already have a verified private recovery point, so use a
// dedicated replacement path and avoid creating a second, less constrained copy.
// This accepts bytes because a failed restore must be able to roll a malformed
// pre-existing file back byte-for-byte without parsing or normalizing it.
fn write_config_atomic_locked(path: &Path, content: &[u8]) -> Result<(), String> {
    let parent = path.parent().ok_or("无法定位 Codex 配置目录")?;
    ensure_existing_directory(parent, "Codex 配置目录")?;
    ensure_regular_config_file_or_absent(path)?;
    crate::modules::codex_config_format::prepare_codex_config_file_for_write(path)
        .map_err(|_| "无法准备 Codex 配置文件写入".to_string())?;
    let file_name = path
        .file_name()
        .and_then(|name| name.to_str())
        .ok_or("无法解析 Codex 配置文件名")?;
    let temporary = parent.join(format!(".{}.xiass-tx-{}.tmp", file_name, Uuid::new_v4()));
    write_private_new_file(&temporary, content, "Codex 配置临时文件")?;
    fs::rename(&temporary, path).map_err(|_| "无法替换 Codex 配置文件".to_string())?;
    set_private_file_permissions(path)?;
    sync_parent_directory(path)?;
    #[cfg(test)]
    if take_test_config_write_failure_after_replace() {
        return Err("测试触发的 Codex 配置替换后失败".to_string());
    }
    Ok(())
}

fn remove_config_locked(path: &Path) -> Result<(), String> {
    ensure_regular_config_file_or_absent(path)?;
    crate::modules::codex_config_format::prepare_codex_config_file_for_write(path)
        .map_err(|_| "无法准备 Codex 配置文件写入".to_string())?;
    match fs::remove_file(path) {
        Ok(()) => sync_parent_directory(path),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(_) => Err("无法恢复 Codex 配置不存在状态".to_string()),
    }
}

fn config_matches_snapshot_contents(
    path: &Path,
    original_existed: bool,
    snapshot: &[u8],
) -> Result<bool, String> {
    ensure_regular_config_file_or_absent(path)?;
    match fs::read(path) {
        Ok(actual) => Ok(original_existed && actual == snapshot),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(!original_existed),
        Err(_) => Err("无法读取 Codex 配置状态".to_string()),
    }
}

fn config_matches_snapshot(
    path: &Path,
    manifest: &CodexConfigBackupInfo,
    snapshot: &[u8],
) -> Result<bool, String> {
    config_matches_snapshot_contents(path, manifest.original_existed, snapshot)
}

fn restore_snapshot_contents_locked(
    path: &Path,
    original_existed: bool,
    snapshot: &[u8],
) -> Result<(), String> {
    if original_existed {
        write_config_atomic_locked(path, snapshot)
            .map_err(|_| "无法恢复 Codex 配置快照".to_string())?;
    } else {
        remove_config_locked(path)?;
    }
    if config_matches_snapshot_contents(path, original_existed, snapshot)? {
        return Ok(());
    }
    Err("Codex 配置恢复复核失败".to_string())
}

fn restore_snapshot_locked(
    path: &Path,
    manifest: &CodexConfigBackupInfo,
    snapshot: &[u8],
) -> Result<(), String> {
    restore_snapshot_contents_locked(path, manifest.original_existed, snapshot)
}

fn restore_verified_snapshot_locked(
    path: &Path,
    backup: &CodexConfigBackupInfo,
) -> Result<(), String> {
    let (_, snapshot) = read_verified_snapshot(path, &backup.id)?;
    restore_snapshot_locked(path, backup, &snapshot)
}

fn restore_private_safety_snapshot_locked(
    path: &Path,
    backup: &PrivateRestoreSafetySnapshot,
) -> Result<(), String> {
    let (_, snapshot) = read_private_restore_safety_snapshot(path, &backup.id)?;
    restore_snapshot_contents_locked(path, backup.original_existed, &snapshot)
}

impl RestoreSafetyBackup {
    fn id(&self) -> &str {
        match self {
            Self::Verified(backup) => &backup.id,
            Self::Private(backup) => &backup.id,
        }
    }

    fn matches_current_config(&self, path: &Path) -> Result<bool, String> {
        match self {
            Self::Verified(backup) => {
                let (_, snapshot) = read_verified_snapshot(path, &backup.id)?;
                config_matches_snapshot(path, backup, &snapshot)
            }
            Self::Private(backup) => {
                let (_, snapshot) = read_private_restore_safety_snapshot(path, &backup.id)?;
                config_matches_snapshot_contents(path, backup.original_existed, &snapshot)
            }
        }
    }

    fn restore_locked(&self, path: &Path) -> Result<(), String> {
        match self {
            Self::Verified(backup) => restore_verified_snapshot_locked(path, backup),
            Self::Private(backup) => restore_private_safety_snapshot_locked(path, backup),
        }
    }
}

fn rollback_after_write_failure(
    path: &Path,
    backup: &CodexConfigBackupInfo,
    cause: &str,
) -> String {
    match read_verified_snapshot(path, &backup.id)
        .and_then(|(_, snapshot)| config_matches_snapshot(path, backup, &snapshot))
    {
        Ok(true) => format!("Codex 配置写入失败，当前配置未改变：{}", cause),
        Ok(false) | Err(_) => match restore_verified_snapshot_locked(path, backup) {
            Ok(()) => format!("Codex 配置写入失败，已恢复原配置：{}", cause),
            Err(_) => "Codex 配置写入失败，且自动恢复未完成".to_string(),
        },
    }
}

fn rollback_after_restore_failure(
    path: &Path,
    safety_backup: &RestoreSafetyBackup,
    cause: &str,
) -> String {
    match safety_backup.matches_current_config(path) {
        Ok(true) => format!("Codex 配置恢复失败，当前配置未改变：{}", cause),
        Ok(false) | Err(_) => match safety_backup.restore_locked(path) {
            Ok(()) => format!("Codex 配置恢复失败，已恢复操作前配置：{}", cause),
            Err(_) => "Codex 配置恢复失败，且操作前配置无法自动恢复".to_string(),
        },
    }
}

fn is_expected_backup_file_name(name: &str) -> bool {
    matches!(name, SNAPSHOT_FILE_NAME | MANIFEST_FILE_NAME)
}

fn remove_backup_directory_if_prunable(root: &Path, backup_id: &str) -> Result<(), String> {
    let directory = root.join(parse_backup_id(backup_id)?);
    ensure_existing_private_directory(&directory, "Codex 配置备份")?;
    let mut expected = HashSet::from([
        SNAPSHOT_FILE_NAME.to_string(),
        MANIFEST_FILE_NAME.to_string(),
    ]);
    let entries = fs::read_dir(&directory).map_err(|_| "无法读取 Codex 配置备份".to_string())?;
    for entry in entries {
        let entry = entry.map_err(|_| "无法读取 Codex 配置备份目录项".to_string())?;
        let name = entry.file_name().to_string_lossy().to_string();
        if !is_expected_backup_file_name(&name) || !expected.remove(&name) {
            return Err("Codex 配置备份包含未预期内容，拒绝自动清理".to_string());
        }
        ensure_existing_private_file(&entry.path(), "Codex 配置备份文件")?;
    }
    if !expected.is_empty() {
        return Err("Codex 配置备份内容不完整，拒绝自动清理".to_string());
    }
    for name in [SNAPSHOT_FILE_NAME, MANIFEST_FILE_NAME] {
        fs::remove_file(directory.join(name))
            .map_err(|_| "无法清理过期 Codex 配置备份文件".to_string())?;
    }
    fs::remove_dir(&directory).map_err(|_| "无法清理过期 Codex 配置备份".to_string())?;
    sync_parent_directory(root)
}

fn prune_completed_backups_locked(path: &Path, protected_ids: &[&str]) -> Result<(), String> {
    let backups = list_codex_config_backups(path)?;
    let root = match existing_backup_root(path)? {
        Some(root) => root,
        None => return Ok(()),
    };
    let protected = protected_ids
        .iter()
        .map(|id| parse_backup_id(id))
        .collect::<Result<HashSet<_>, _>>()?;
    let mut retained = 0usize;
    for backup in backups {
        if protected.contains(&backup.id) {
            continue;
        }
        if retained < MAX_COMPLETED_BACKUPS {
            retained += 1;
            continue;
        }
        remove_backup_directory_if_prunable(&root, &backup.id)?;
    }
    Ok(())
}

fn prune_completed_backups_best_effort(path: &Path, protected_ids: &[&str]) {
    if let Err(error) = prune_completed_backups_locked(path, protected_ids) {
        crate::modules::logger::log_warn(&format!(
            "[Codex Config] 清理过期私有配置备份失败，已保留现有恢复点: {}",
            error
        ));
    }
}

pub fn write_codex_config_transactional(
    path: &Path,
    content: &str,
    source: &str,
) -> Result<CodexConfigBackupInfo, String> {
    let path = resolve_config_target(path, true)?;
    let lock = transaction_lock(&path)?;
    let _guard = lock
        .lock()
        .map_err(|_| "Codex 配置事务锁已损坏".to_string())?;
    let backup = create_backup_locked(&path, source)?;
    if let Err(error) = write_config_atomic_locked(&path, content.as_bytes()) {
        return Err(rollback_after_write_failure(&path, &backup, &error));
    }
    match fs::read(&path) {
        Ok(actual) if actual == content.as_bytes() => {
            prune_completed_backups_best_effort(&path, &[]);
            Ok(backup)
        }
        Ok(_) => Err(rollback_after_write_failure(
            &path,
            &backup,
            "Codex 配置写入复核失败",
        )),
        Err(_) => Err(rollback_after_write_failure(
            &path,
            &backup,
            "Codex 配置写入后无法复核",
        )),
    }
}

pub fn list_codex_config_backups(path: &Path) -> Result<Vec<CodexConfigBackupInfo>, String> {
    let path = match resolve_existing_config_target(path)? {
        Some(path) => path,
        None => return Ok(Vec::new()),
    };
    let root = match existing_backup_root(&path)? {
        Some(root) => root,
        None => return Ok(Vec::new()),
    };
    ensure_existing_private_directory(&root, "Codex 配置备份目录")?;
    let entries = fs::read_dir(&root).map_err(|_| "无法列出 Codex 配置备份".to_string())?;
    let mut backups = Vec::new();
    for entry in entries.flatten() {
        let directory = entry.path();
        let Ok(metadata) = fs::symlink_metadata(&directory) else {
            continue;
        };
        if !metadata.is_dir() || is_unsafe_link(&metadata) {
            continue;
        }
        let id = match directory
            .file_name()
            .and_then(|name| name.to_str())
            .and_then(|name| Uuid::parse_str(name).ok())
        {
            Some(id) => id.to_string(),
            None => continue,
        };
        if let Ok((manifest, _)) = read_verified_snapshot(&path, &id) {
            backups.push(manifest);
        }
    }
    backups.sort_by(|left, right| {
        right
            .created_at
            .cmp(&left.created_at)
            .then_with(|| right.id.cmp(&left.id))
    });
    Ok(backups)
}

pub fn verify_codex_config_backup(
    path: &Path,
    backup_id: &str,
) -> Result<CodexConfigBackupVerification, String> {
    let id = parse_backup_id(backup_id)?;
    let Some(path) = resolve_existing_config_target(path)? else {
        return Ok(CodexConfigBackupVerification {
            id,
            valid: false,
            message: "Codex 配置备份未通过校验。".to_string(),
        });
    };
    match read_verified_snapshot(&path, &id) {
        Ok((manifest, _)) => Ok(CodexConfigBackupVerification {
            id: manifest.id,
            valid: true,
            message: "Codex 配置备份已通过 SHA-256 与 TOML 校验。".to_string(),
        }),
        Err(_) => Ok(CodexConfigBackupVerification {
            id,
            valid: false,
            message: "Codex 配置备份未通过校验。".to_string(),
        }),
    }
}

pub fn restore_codex_config_backup(
    path: &Path,
    backup_id: &str,
) -> Result<CodexConfigRestoreResult, String> {
    let path = resolve_config_target(path, false)?;
    let lock = transaction_lock(&path)?;
    let _guard = lock
        .lock()
        .map_err(|_| "Codex 配置事务锁已损坏".to_string())?;
    let (manifest, snapshot) = read_verified_snapshot(&path, backup_id)?;
    let safety_backup = create_restore_safety_backup_locked(&path)?;
    if let Err(error) = restore_snapshot_locked(&path, &manifest, &snapshot) {
        return Err(rollback_after_restore_failure(
            &path,
            &safety_backup,
            &error,
        ));
    }
    let safety_backup_id = safety_backup.id().to_string();
    prune_completed_backups_best_effort(&path, &[&manifest.id, &safety_backup_id]);
    Ok(CodexConfigRestoreResult {
        restored_backup_id: manifest.id,
        safety_backup_id,
        restored: true,
    })
}

#[cfg(test)]
mod tests {
    use super::{
        fail_next_config_write_after_replace_for_test, list_codex_config_backups,
        restore_codex_config_backup, verify_codex_config_backup, write_codex_config_transactional,
        BACKUP_DIRECTORY_NAME, MANIFEST_FILE_NAME, MAX_COMPLETED_BACKUPS,
        PRIVATE_RESTORE_SAFETY_SNAPSHOT_KIND, SNAPSHOT_FILE_NAME,
    };
    use std::fs;
    use std::path::PathBuf;
    use uuid::Uuid;

    fn temporary_config_path(label: &str) -> PathBuf {
        let directory = std::env::temp_dir().join(format!(
            "xiass-codex-config-tx-{}-{}",
            label,
            Uuid::new_v4()
        ));
        fs::create_dir_all(&directory).expect("create temporary config directory");
        directory.join("config.toml")
    }

    fn backup_directory(path: &std::path::Path, id: &str) -> PathBuf {
        path.parent()
            .expect("temporary directory")
            .join(BACKUP_DIRECTORY_NAME)
            .join(id)
    }

    fn private_safety_backup_ids(path: &std::path::Path) -> Vec<String> {
        let root = path
            .parent()
            .expect("temporary directory")
            .join(BACKUP_DIRECTORY_NAME);
        fs::read_dir(root)
            .expect("list backup directories")
            .flatten()
            .filter_map(|entry| {
                let manifest = fs::read(entry.path().join(MANIFEST_FILE_NAME)).ok()?;
                let manifest = serde_json::from_slice::<serde_json::Value>(&manifest).ok()?;
                (manifest.get("kind").and_then(|value| value.as_str())
                    == Some(PRIVATE_RESTORE_SAFETY_SNAPSHOT_KIND))
                .then(|| {
                    manifest
                        .get("id")
                        .and_then(|value| value.as_str())
                        .map(str::to_string)
                })
                .flatten()
            })
            .collect()
    }

    #[test]
    fn transactional_write_creates_verified_versioned_backup_and_restore_recovers_original() {
        let path = temporary_config_path("backup-restore");
        fs::write(&path, "model = \"before\"\n").expect("write original config");

        let backup = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("transactional write");
        assert!(backup.valid);
        assert_eq!(backup.source, "quick-config");
        assert_ne!(backup.sha256, "");
        assert_eq!(
            fs::read_to_string(&path).expect("read changed config"),
            "model = \"after\"\n"
        );
        assert!(
            !path.with_file_name("config.toml.bak").exists(),
            "the transaction must not create an additional legacy plaintext backup"
        );

        let verification = verify_codex_config_backup(&path, &backup.id).expect("verify backup");
        assert!(verification.valid);
        let listed = list_codex_config_backups(&path).expect("list backups");
        assert_eq!(listed.len(), 1);
        assert_eq!(listed[0].id, backup.id);

        let restored = restore_codex_config_backup(&path, &backup.id).expect("restore backup");
        assert!(restored.restored);
        assert_ne!(restored.safety_backup_id, backup.id);
        assert_eq!(
            fs::read_to_string(&path).expect("read restored config"),
            "model = \"before\"\n"
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[test]
    fn restore_accepts_a_malformed_current_config_and_keeps_a_private_byte_exact_safety_snapshot() {
        let path = temporary_config_path("restore-malformed-current");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let target = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("create target recovery point");
        let malformed = b"model = [\ninvalid = \xff\n";
        fs::write(&path, malformed).expect("write malformed current config");

        let restored =
            restore_codex_config_backup(&path, &target.id).expect("restore verified target");
        assert!(restored.restored);
        assert_eq!(restored.restored_backup_id, target.id);
        assert_eq!(
            fs::read_to_string(&path).expect("read restored config"),
            "model = \"before\"\n"
        );
        assert_eq!(
            fs::read(backup_directory(&path, &restored.safety_backup_id).join(SNAPSHOT_FILE_NAME))
                .expect("read private safety snapshot"),
            malformed
        );
        assert!(
            !verify_codex_config_backup(&path, &restored.safety_backup_id)
                .expect("verify private safety snapshot")
                .valid,
            "a malformed safety snapshot must not be exposed as a restorable public point"
        );
        assert!(
            list_codex_config_backups(&path)
                .expect("list public backups")
                .iter()
                .all(|backup| backup.id != restored.safety_backup_id),
            "private malformed bytes must never appear in the frontend recovery-point list"
        );
        assert!(
            verify_codex_config_backup(&path, &target.id)
                .expect("reverify target")
                .valid,
            "the selected target must retain its SHA-256 and TOML verification"
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[test]
    fn failed_restore_rolls_back_a_malformed_current_config_from_the_private_safety_snapshot() {
        let path = temporary_config_path("restore-malformed-rollback");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let target = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("create target recovery point");
        let malformed = b"model = [\ninvalid = \xff\n";
        fs::write(&path, malformed).expect("write malformed current config");

        fail_next_config_write_after_replace_for_test();
        let error = restore_codex_config_backup(&path, &target.id)
            .expect_err("forced post-replace restore error must be reported");
        assert!(error.contains("已恢复操作前配置"));
        assert_eq!(
            fs::read(&path).expect("read rolled back malformed config"),
            malformed,
            "rollback must restore the malformed file exactly, including invalid UTF-8 bytes"
        );

        let private_ids = private_safety_backup_ids(&path);
        assert_eq!(
            private_ids.len(),
            1,
            "one private safety snapshot is expected"
        );
        let private_id = &private_ids[0];
        assert_eq!(
            fs::read(backup_directory(&path, private_id).join(SNAPSHOT_FILE_NAME))
                .expect("read private safety snapshot"),
            malformed
        );
        assert!(
            !verify_codex_config_backup(&path, private_id)
                .expect("verify private safety snapshot")
                .valid
        );
        assert!(
            restore_codex_config_backup(&path, private_id).is_err(),
            "a raw malformed safety snapshot cannot be selected as a public restore target"
        );
        assert!(
            verify_codex_config_backup(&path, &target.id)
                .expect("reverify target")
                .valid
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[test]
    fn unverified_restore_target_never_snapshots_or_changes_a_malformed_current_config() {
        let path = temporary_config_path("restore-target-verification");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let target = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("create target recovery point");
        fs::write(
            backup_directory(&path, &target.id).join(SNAPSHOT_FILE_NAME),
            "model = \"tampered\"\n",
        )
        .expect("tamper target snapshot");
        let malformed = b"model = [\ninvalid = \xff\n";
        fs::write(&path, malformed).expect("write malformed current config");

        assert!(
            !verify_codex_config_backup(&path, &target.id)
                .expect("verify tampered target")
                .valid
        );
        assert!(restore_codex_config_backup(&path, &target.id).is_err());
        assert_eq!(
            fs::read(&path).expect("read untouched malformed config"),
            malformed
        );
        assert!(
            private_safety_backup_ids(&path).is_empty(),
            "target verification must complete before a current-config safety snapshot is created"
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[test]
    fn backup_creation_failure_stops_write_before_current_config_changes() {
        let path = temporary_config_path("backup-failure");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let root = path
            .parent()
            .expect("temporary directory")
            .join(BACKUP_DIRECTORY_NAME);
        fs::write(&root, "not a directory").expect("block backup root");

        assert!(
            write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config").is_err()
        );
        assert_eq!(
            fs::read_to_string(&path).expect("read unchanged config"),
            "model = \"before\"\n"
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[test]
    fn restore_rejects_tampered_snapshot_without_changing_current_config() {
        let path = temporary_config_path("tamper");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let backup = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("transactional write");
        fs::write(
            backup_directory(&path, &backup.id).join(SNAPSHOT_FILE_NAME),
            "model = \"tampered\"\n",
        )
        .expect("tamper snapshot");

        let verification =
            verify_codex_config_backup(&path, &backup.id).expect("verify tampered backup");
        assert!(!verification.valid);
        assert!(restore_codex_config_backup(&path, &backup.id).is_err());
        assert_eq!(
            fs::read_to_string(&path).expect("read unchanged config"),
            "model = \"after\"\n"
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[test]
    fn completed_backups_are_retained_at_a_bounded_count() {
        let path = temporary_config_path("retention");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        for index in 0..(MAX_COMPLETED_BACKUPS + 3) {
            write_codex_config_transactional(
                &path,
                &format!("model = \"after-{}\"\n", index),
                "quick-config",
            )
            .expect("transactional write");
        }
        assert_eq!(
            list_codex_config_backups(&path)
                .expect("list retained backups")
                .len(),
            MAX_COMPLETED_BACKUPS
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[cfg(unix)]
    #[test]
    fn backup_tree_is_private_and_rejects_symbolic_link_roots() {
        use std::os::unix::fs::{symlink, PermissionsExt};

        let path = temporary_config_path("private-permissions");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let backup = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("transactional write");
        let root = path
            .parent()
            .expect("temporary directory")
            .join(BACKUP_DIRECTORY_NAME);
        let directory = backup_directory(&path, &backup.id);
        assert_eq!(
            fs::metadata(&root)
                .expect("root metadata")
                .permissions()
                .mode()
                & 0o777,
            0o700
        );
        assert_eq!(
            fs::metadata(&directory)
                .expect("backup directory metadata")
                .permissions()
                .mode()
                & 0o777,
            0o700
        );
        for name in [SNAPSHOT_FILE_NAME, MANIFEST_FILE_NAME] {
            assert_eq!(
                fs::metadata(directory.join(name))
                    .expect("backup file metadata")
                    .permissions()
                    .mode()
                    & 0o777,
                0o600
            );
        }

        let linked_path = temporary_config_path("symlink-root");
        fs::write(&linked_path, "model = \"before\"\n").expect("write linked config");
        let linked_root = linked_path
            .parent()
            .expect("temporary directory")
            .join(BACKUP_DIRECTORY_NAME);
        symlink(&root, &linked_root).expect("create backup root symlink");
        assert!(write_codex_config_transactional(
            &linked_path,
            "model = \"after\"\n",
            "quick-config"
        )
        .is_err());
        assert_eq!(
            fs::read_to_string(&linked_path).expect("read unchanged linked config"),
            "model = \"before\"\n"
        );

        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
        let _ = fs::remove_dir_all(linked_path.parent().expect("temporary directory"));
    }

    #[cfg(unix)]
    #[test]
    fn restore_rejects_symbolic_link_snapshot_without_changing_current_config() {
        use std::os::unix::fs::symlink;

        let path = temporary_config_path("symlink-snapshot");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let backup = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("transactional write");
        let snapshot = backup_directory(&path, &backup.id).join(SNAPSHOT_FILE_NAME);
        let redirected = path
            .parent()
            .expect("temporary directory")
            .join("outside.toml");
        fs::write(&redirected, "model = \"outside\"\n").expect("write redirected file");
        fs::remove_file(&snapshot).expect("remove snapshot");
        symlink(&redirected, &snapshot).expect("create snapshot symlink");

        assert!(
            !verify_codex_config_backup(&path, &backup.id)
                .expect("verify linked snapshot")
                .valid
        );
        assert!(restore_codex_config_backup(&path, &backup.id).is_err());
        assert_eq!(
            fs::read_to_string(&path).expect("read unchanged config"),
            "model = \"after\"\n"
        );
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }

    #[cfg(unix)]
    #[test]
    fn write_rejects_a_direct_symbolic_linked_config_parent() {
        use std::os::unix::fs::symlink;

        let root = temporary_config_path("symlink-parent");
        let root = root.parent().expect("temporary directory").to_path_buf();
        let actual_parent = root.join("actual-codex-home");
        let linked_parent = root.join("linked-codex-home");
        fs::create_dir_all(&actual_parent).expect("create actual config parent");
        let actual_config = actual_parent.join("config.toml");
        fs::write(&actual_config, "model = \"before\"\n").expect("write actual config");
        symlink(&actual_parent, &linked_parent).expect("create direct parent symlink");
        let requested_path = linked_parent.join("config.toml");

        assert!(write_codex_config_transactional(
            &requested_path,
            "model = \"after\"\n",
            "quick-config"
        )
        .is_err());
        assert_eq!(
            fs::read_to_string(&actual_config).expect("read unchanged actual config"),
            "model = \"before\"\n"
        );
        assert!(
            !actual_parent.join(BACKUP_DIRECTORY_NAME).exists(),
            "a rejected direct parent link must not create a backup through its target"
        );
        let _ = fs::remove_dir_all(&root);
    }

    #[cfg(unix)]
    #[test]
    fn write_and_restore_reject_a_symbolic_linked_config_file() {
        use std::os::unix::fs::symlink;

        let path = temporary_config_path("symlink-config");
        fs::write(&path, "model = \"before\"\n").expect("write original config");
        let backup = write_codex_config_transactional(&path, "model = \"after\"\n", "quick-config")
            .expect("transactional write");
        let redirected = path
            .parent()
            .expect("temporary directory")
            .join("redirected-config.toml");
        fs::write(&redirected, "model = \"external\"\n").expect("write redirected config");
        fs::remove_file(&path).expect("remove configured file");
        symlink(&redirected, &path).expect("replace config with symbolic link");

        assert!(
            write_codex_config_transactional(&path, "model = \"new\"\n", "quick-config").is_err()
        );
        assert!(restore_codex_config_backup(&path, &backup.id).is_err());
        assert_eq!(
            fs::read_to_string(&redirected).expect("read external target"),
            "model = \"external\"\n"
        );
        assert!(fs::symlink_metadata(&path)
            .expect("linked config metadata")
            .file_type()
            .is_symlink());
        let _ = fs::remove_dir_all(path.parent().expect("temporary directory"));
    }
}
