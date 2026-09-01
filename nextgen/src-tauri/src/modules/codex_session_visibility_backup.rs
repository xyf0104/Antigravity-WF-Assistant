// Codex Session Visibility：Atomic rollout/SQLite backup and restore operations。
// 通过 include! 保持原模块作用域、私有调用关系和修复事务行为。
const SESSION_VISIBILITY_REPAIR_MANIFEST_FILE: &str = "manifest.json";
const SESSION_VISIBILITY_REPAIR_OPERATION_VERSION: u8 = 2;
const MAX_SESSION_VISIBILITY_REPAIR_MANIFEST_BYTES: u64 = 128 * 1024;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum SessionVisibilityRepairOperationStatus {
    Prepared,
    Applying,
    Committed,
    RolledBack,
}

impl Default for SessionVisibilityRepairOperationStatus {
    fn default() -> Self {
        // Backups written before the operation journal existed were created
        // only after the repair completed successfully. Treat them as
        // terminal so an upgrade never replays an old backup.
        Self::Committed
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct SessionVisibilityRepairOperationManifest {
    #[serde(default = "legacy_session_visibility_repair_manifest_version")]
    version: u8,
    instance_id: String,
    instance_root: String,
    target_provider: String,
    created_at: String,
    #[serde(default)]
    instance_scope: String,
    #[serde(default)]
    operation_status: SessionVisibilityRepairOperationStatus,
    #[serde(default)]
    operation_updated_at: String,
    #[serde(default)]
    operation_message: String,
    #[serde(default)]
    has_sqlite_backup: bool,
    #[serde(default)]
    sqlite_files: Vec<String>,
    #[serde(default)]
    has_session_index_backup: bool,
    #[serde(default)]
    has_global_state_backup: bool,
    #[serde(default)]
    rollout_files: Vec<String>,
}

fn legacy_session_visibility_repair_manifest_version() -> u8 {
    1
}

fn session_visibility_repair_backup_scope_dir(data_dir: &Path) -> Result<PathBuf, String> {
    let root = modules::backup_storage::get_backup_root_dir()?;
    Ok(root
        .join("behavior")
        .join("codex")
        .join(modules::backup_storage::scope_for_path(data_dir)))
}

fn is_session_visibility_repair_backup_name(name: &str) -> bool {
    name.starts_with(SESSION_VISIBILITY_REPAIR_BACKUP_PREFIX)
        && name.ends_with(SESSION_VISIBILITY_REPAIR_BACKUP_SUFFIX)
}

fn validate_session_visibility_repair_backup_dir(
    scope_dir: &Path,
    backup_dir: &Path,
) -> Result<(), String> {
    if backup_dir.parent() != Some(scope_dir) {
        return Err("会话可见性修复备份目录不属于当前实例范围".to_string());
    }
    let name = backup_dir
        .file_name()
        .and_then(|value| value.to_str())
        .ok_or_else(|| "会话可见性修复备份目录名无效".to_string())?;
    if !is_session_visibility_repair_backup_name(name) {
        return Err("会话可见性修复备份目录名无效".to_string());
    }
    let metadata = fs::symlink_metadata(backup_dir)
        .map_err(|_| "无法读取会话可见性修复备份目录".to_string())?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() {
        return Err("会话可见性修复备份目录不安全".to_string());
    }
    Ok(())
}

fn operation_manifest_path(backup_dir: &Path) -> PathBuf {
    backup_dir.join(SESSION_VISIBILITY_REPAIR_MANIFEST_FILE)
}

fn read_session_visibility_repair_operation_manifest(
    scope_dir: &Path,
    backup_dir: &Path,
) -> Result<SessionVisibilityRepairOperationManifest, String> {
    validate_session_visibility_repair_backup_dir(scope_dir, backup_dir)?;
    let manifest_path = operation_manifest_path(backup_dir);
    let metadata = fs::symlink_metadata(&manifest_path)
        .map_err(|_| "会话可见性修复操作清单不存在".to_string())?;
    if !metadata.is_file()
        || metadata.file_type().is_symlink()
        || metadata.len() > MAX_SESSION_VISIBILITY_REPAIR_MANIFEST_BYTES
    {
        return Err("会话可见性修复操作清单不安全".to_string());
    }
    let manifest = serde_json::from_slice::<SessionVisibilityRepairOperationManifest>(
        &fs::read(&manifest_path).map_err(|_| "无法读取会话可见性修复操作清单".to_string())?,
    )
    .map_err(|_| "会话可见性修复操作清单无效".to_string())?;
    if manifest.version > SESSION_VISIBILITY_REPAIR_OPERATION_VERSION
        || manifest.instance_id.trim().is_empty()
        || manifest.instance_root.trim().is_empty()
        || manifest.target_provider.trim().is_empty()
    {
        return Err("会话可见性修复操作清单校验失败".to_string());
    }
    Ok(manifest)
}

fn write_session_visibility_repair_operation_manifest(
    backup_dir: &Path,
    manifest: &SessionVisibilityRepairOperationManifest,
) -> Result<(), String> {
    let serialized = serde_json::to_string_pretty(manifest)
        .map_err(|_| "序列化会话可见性修复操作清单失败".to_string())?;
    modules::atomic_write::write_string_atomic(
        &operation_manifest_path(backup_dir),
        &format!("{serialized}\n"),
    )
    .map_err(|error| format!("写入会话可见性修复操作清单失败: {error}"))
}

fn set_session_visibility_repair_operation_status(
    scope_dir: &Path,
    backup_dir: &Path,
    status: SessionVisibilityRepairOperationStatus,
    message: &str,
) -> Result<(), String> {
    let mut manifest = read_session_visibility_repair_operation_manifest(scope_dir, backup_dir)?;
    manifest.operation_status = status;
    manifest.operation_updated_at = Utc::now().to_rfc3339();
    manifest.operation_message = message.trim().chars().take(240).collect();
    write_session_visibility_repair_operation_manifest(backup_dir, &manifest)
}

fn recover_incomplete_session_visibility_repairs(
    data_dir: &Path,
    instance_id: &str,
    instance_running: bool,
) -> Result<(), String> {
    let scope_dir = session_visibility_repair_backup_scope_dir(data_dir)?;
    recover_incomplete_session_visibility_repairs_in_scope(
        data_dir,
        instance_id,
        instance_running,
        &scope_dir,
    )
}

fn recover_incomplete_session_visibility_repairs_in_scope(
    data_dir: &Path,
    instance_id: &str,
    instance_running: bool,
    scope_dir: &Path,
) -> Result<(), String> {
    let scope_metadata = match fs::symlink_metadata(scope_dir) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(_) => return Err("无法读取会话可见性修复备份范围".to_string()),
    };
    if !scope_metadata.is_dir() || scope_metadata.file_type().is_symlink() {
        return Err("会话可见性修复备份范围不安全".to_string());
    }

    let mut candidates = fs::read_dir(scope_dir)
        .map_err(|_| "无法列出会话可见性修复备份".to_string())?
        .filter_map(Result::ok)
        .filter_map(|entry| {
            let file_type = entry.file_type().ok()?;
            let file_name = entry.file_name();
            let name = file_name.to_str()?;
            (file_type.is_dir()
                && !file_type.is_symlink()
                && is_session_visibility_repair_backup_name(name))
            .then_some(entry.path())
        })
        .collect::<Vec<_>>();
    candidates.sort_by(|left, right| right.file_name().cmp(&left.file_name()));

    for backup_dir in candidates {
        let manifest =
            match read_session_visibility_repair_operation_manifest(scope_dir, &backup_dir) {
                Ok(manifest) => manifest,
                Err(error) => {
                    modules::logger::log_warn(&format!(
                    "[Codex Session Visibility] 跳过无法验证的历史修复备份: backup={}, error={}",
                    backup_dir.display(),
                    error
                ));
                    continue;
                }
            };
        // Version 1 has no state marker. It was created by the older completed
        // flow and must never be treated as an interrupted operation.
        if manifest.version < SESSION_VISIBILITY_REPAIR_OPERATION_VERSION
            || manifest.instance_scope != modules::backup_storage::scope_for_path(data_dir)
        {
            continue;
        }
        if manifest.instance_id != instance_id {
            modules::logger::log_warn(&format!(
                "[Codex Session Visibility] 跳过实例 ID 不匹配的历史修复备份: backup={}",
                backup_dir.display()
            ));
            continue;
        }

        match manifest.operation_status {
            SessionVisibilityRepairOperationStatus::Prepared => {
                set_session_visibility_repair_operation_status(
                    scope_dir,
                    &backup_dir,
                    SessionVisibilityRepairOperationStatus::RolledBack,
                    "恢复：上次修复在写入前结束",
                )?;
                modules::logger::log_info(&format!(
                    "[Codex Session Visibility] 已关闭未写入的历史修复操作: backup={}",
                    backup_dir.display()
                ));
            }
            SessionVisibilityRepairOperationStatus::Applying => {
                if instance_running {
                    return Err(format!(
                        "检测到未完成的 Codex 历史会话修复；请先退出 {} 后重试，以便安全恢复备份",
                        instance_id
                    ));
                }
                restore_instance_files_from_backup(
                    data_dir,
                    &backup_dir,
                    manifest.has_sqlite_backup,
                )
                .map_err(|error| {
                    format!(
                        "恢复未完成的 Codex 历史会话修复失败；已保留备份以便再次尝试: {}",
                        error
                    )
                })?;
                set_session_visibility_repair_operation_status(
                    scope_dir,
                    &backup_dir,
                    SessionVisibilityRepairOperationStatus::RolledBack,
                    "恢复：已从未完成修复前的备份自动回滚",
                )?;
                modules::logger::log_info(&format!(
                    "[Codex Session Visibility] 已自动恢复未完成的历史修复: backup={}",
                    backup_dir.display()
                ));
            }
            SessionVisibilityRepairOperationStatus::Committed
            | SessionVisibilityRepairOperationStatus::RolledBack => {}
        }
    }

    Ok(())
}
fn format_sqlite_write_error(path: &Path, error: &rusqlite::Error) -> String {
    let message = error.to_string();
    let lowered = message.to_ascii_lowercase();
    if lowered.contains("database is locked") || lowered.contains("database busy") {
        return format!(
            "Codex SQLite 会话库当前被占用，请关闭 Codex / Codex App 后重试 ({}): {}",
            path.display(),
            message
        );
    }
    format!(
        "更新 SQLite 会话可见性失败 ({}): {}",
        path.display(),
        message
    )
}

fn rewrite_rollout_provider(change: &RolloutProviderChange) -> Result<bool, String> {
    let metadata = match fs::metadata(&change.absolute_path) {
        Ok(metadata) => metadata,
        Err(error) => {
            modules::logger::log_warn(&format!(
                "跳过已不可读取的 Codex rollout 文件 ({}): {}",
                change.absolute_path.display(),
                error
            ));
            return Ok(false);
        }
    };
    let current_modified_at = metadata.modified().ok();
    if metadata.len() != change.source_size
        || !modules::codex_session_file_time::same_modified_time_millis(
            current_modified_at,
            change.source_modified_at,
        )
    {
        modules::logger::log_warn(&format!(
            "跳过修复扫描后发生变化的 Codex rollout 文件: {}",
            change.absolute_path.display()
        ));
        return Ok(false);
    }
    let original_modified_at =
        modules::codex_session_file_time::read_modified_time(&change.absolute_path);
    if let Some(updated_content) = change.updated_content.as_ref() {
        match updated_content {
            RolloutProviderUpdate::FullContent(content) => {
                write_bytes_atomic(&change.absolute_path, content.as_bytes())?;
            }
            RolloutProviderUpdate::FirstLine(line) => {
                rewrite_rollout_first_line(&change.absolute_path, line)?;
            }
        }
    }
    modules::codex_session_file_time::restore_modified_time(
        &change.absolute_path,
        change.target_modified_at.or(original_modified_at),
    )?;
    Ok(true)
}

fn rewrite_rollout_first_line(path: &Path, updated_first_line: &str) -> Result<(), String> {
    let content = fs::read_to_string(path)
        .map_err(|error| format!("读取 rollout 文件失败 ({}): {}", path.display(), error))?;
    let (first_segment, rest) = match content.find('\n') {
        Some(index) => (&content[..index + 1], &content[index + 1..]),
        None => (content.as_str(), ""),
    };
    let (_, separator) = split_line_ending(first_segment);
    let mut output = String::new();
    output.push_str(updated_first_line);
    output.push_str(separator);
    output.push_str(rest);
    write_bytes_atomic(path, output.as_bytes())
}

fn write_bytes_atomic(path: &Path, content: &[u8]) -> Result<(), String> {
    let parent = path
        .parent()
        .ok_or_else(|| format!("无法定位目标目录: {}", path.display()))?;
    fs::create_dir_all(parent)
        .map_err(|error| format!("创建目录失败 ({}): {}", parent.display(), error))?;

    let temp_path = parent.join(format!(
        ".{}.provider-repair.{}.{}",
        path.file_name()
            .and_then(|item| item.to_str())
            .unwrap_or("file"),
        std::process::id(),
        Utc::now().timestamp_nanos_opt().unwrap_or_default()
    ));
    fs::write(&temp_path, content)
        .map_err(|error| format!("写入临时文件失败 ({}): {}", temp_path.display(), error))?;
    if let Err(error) = fs::rename(&temp_path, path) {
        let _ = fs::remove_file(&temp_path);
        return Err(format!("替换文件失败 ({}): {}", path.display(), error));
    }
    Ok(())
}

fn sqlite_candidate_paths(data_dir: &Path) -> Vec<PathBuf> {
    let mut paths = sqlite_dir_session_dbs(data_dir);
    let legacy = data_dir.join(STATE_DB_FILE);
    if !paths.iter().any(|path| path == &legacy) {
        paths.push(legacy);
    }
    paths
}

fn sqlite_candidate_paths_for_options(
    data_dir: &Path,
    options: CodexSessionVisibilityRepairOptions,
) -> Vec<PathBuf> {
    match options.sqlite_scope {
        SqliteRepairScope::LegacyStateOnly => vec![data_dir.join(STATE_DB_FILE)],
        SqliteRepairScope::OfficialStateDbs => official_state_db_candidate_paths(data_dir),
        SqliteRepairScope::AllSessionDbs => provider_sync_sqlite_paths(data_dir),
    }
}

fn provider_sync_sqlite_paths(data_dir: &Path) -> Vec<PathBuf> {
    sqlite_candidate_paths(data_dir)
        .into_iter()
        .filter(|path| has_provider_sync_table(path))
        .collect()
}

fn official_state_db_candidate_paths(data_dir: &Path) -> Vec<PathBuf> {
    let mut paths = Vec::new();
    push_unique_path(
        &mut paths,
        data_dir.join(SQLITE_DIR_NAME).join(OFFICIAL_STATE_DB_FILE),
    );
    push_unique_path(&mut paths, data_dir.join(STATE_DB_FILE));
    paths
}

fn push_unique_path(paths: &mut Vec<PathBuf>, path: PathBuf) {
    if !paths.iter().any(|existing| existing == &path) {
        paths.push(path);
    }
}

fn sqlite_dir_session_dbs(data_dir: &Path) -> Vec<PathBuf> {
    let sqlite_dir = data_dir.join(SQLITE_DIR_NAME);
    let Ok(entries) = fs::read_dir(&sqlite_dir) else {
        return Vec::new();
    };
    let mut candidates = entries
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| path.is_file())
        .filter(|path| is_sqlite_candidate(path))
        .filter(|path| has_codex_session_table(path))
        .collect::<Vec<_>>();
    candidates.sort_by_key(|path| {
        (
            path.file_name()
                .map(|name| name != OsStr::new(PREFERRED_SQLITE_DB_FILE))
                .unwrap_or(true),
            path.file_name().map(|name| name.to_os_string()),
        )
    });
    candidates
}

fn is_sqlite_candidate(path: &Path) -> bool {
    matches!(
        path.extension().and_then(OsStr::to_str),
        Some("db") | Some("sqlite") | Some("sqlite3")
    )
}

fn has_codex_session_table(path: &Path) -> bool {
    let Ok(connection) = Connection::open_with_flags(path, OpenFlags::SQLITE_OPEN_READ_ONLY) else {
        return false;
    };
    [
        "threads",
        "local_thread_catalog",
        "automation_runs",
        "inbox_items",
    ]
    .iter()
    .any(|table| {
        connection
            .query_row(
                "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?1 LIMIT 1",
                [table],
                |_| Ok(()),
            )
            .is_ok()
    })
}

fn has_provider_sync_table(path: &Path) -> bool {
    let Ok(connection) = Connection::open_with_flags(path, OpenFlags::SQLITE_OPEN_READ_ONLY) else {
        return false;
    };
    ["threads", "local_thread_catalog"].iter().any(|table| {
        connection
            .query_row(
                "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?1 LIMIT 1",
                [table],
                |_| Ok(()),
            )
            .is_ok()
    })
}

fn relative_to_instance_root(data_dir: &Path, path: &Path) -> PathBuf {
    path.strip_prefix(data_dir).unwrap_or(path).to_path_buf()
}

fn sqlite_sidecar_paths(db_path: &Path) -> Vec<PathBuf> {
    let raw = db_path.to_string_lossy();
    vec![
        PathBuf::from(format!("{}-wal", raw)),
        PathBuf::from(format!("{}-shm", raw)),
    ]
}

fn remove_sqlite_sidecar_files(db_path: &Path) -> Result<(), String> {
    for path in sqlite_sidecar_paths(db_path) {
        match fs::remove_file(&path) {
            Ok(()) => {}
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => {
                return Err(format!(
                    "清理 SQLite sidecar 文件失败 ({}): {}",
                    path.display(),
                    error
                ));
            }
        }
    }
    Ok(())
}

fn backup_sqlite_databases(
    data_dir: &Path,
    backup_dir: &Path,
    options: CodexSessionVisibilityRepairOptions,
) -> Result<Vec<String>, String> {
    let mut backed_up = Vec::new();
    for db_path in sqlite_candidate_paths_for_options(data_dir, options) {
        if !db_path.exists() {
            continue;
        }
        let relative = relative_to_instance_root(data_dir, &db_path);
        let backup_db_path = backup_dir.join("db").join(&relative);
        if let Some(parent) = backup_db_path.parent() {
            fs::create_dir_all(parent).map_err(|error| {
                format!("创建 SQLite 备份目录失败 ({}): {}", parent.display(), error)
            })?;
        }
        let connection = Connection::open(&db_path).map_err(|error| {
            format!(
                "打开 SQLite 会话库以创建一致备份失败 ({}): {}",
                db_path.display(),
                error
            )
        })?;
        connection
            .busy_timeout(Duration::from_secs(3))
            .map_err(|error| {
                format!(
                    "设置 SQLite 备份 busy_timeout 失败 ({}): {}",
                    db_path.display(),
                    error
                )
            })?;

        if backup_db_path.exists() {
            fs::remove_file(&backup_db_path).map_err(|error| {
                format!(
                    "删除旧 SQLite 备份失败 ({}): {}",
                    backup_db_path.display(),
                    error
                )
            })?;
        }
        let backup_target = backup_db_path.to_string_lossy().to_string();
        connection
            .execute("VACUUM main INTO ?1", [backup_target.as_str()])
            .map_err(|error| {
                format!(
                    "备份 SQLite 会话库失败 ({} -> {}): {}",
                    db_path.display(),
                    backup_db_path.display(),
                    error
                )
            })?;
        backed_up.push(relative.to_string_lossy().replace('\\', "/"));
    }
    Ok(backed_up)
}

fn restore_sqlite_databases_from_backup(
    data_dir: &Path,
    backup_dir: &Path,
) -> Result<Vec<String>, String> {
    let backup_db_root = backup_dir.join("db");
    if !backup_db_root.exists() {
        return Ok(Vec::new());
    }
    let backup_paths = list_backup_sqlite_files(&backup_db_root)?;
    let mut restored = Vec::new();
    for backup_db_path in backup_paths {
        let relative = backup_db_path
            .strip_prefix(&backup_db_root)
            .map_err(|_| format!("无法计算 SQLite 备份相对路径: {}", backup_db_path.display()))?;
        let target_db_path = data_dir.join(relative);
        if let Some(parent) = target_db_path.parent() {
            fs::create_dir_all(parent).map_err(|error| {
                format!("创建 SQLite 恢复目录失败 ({}): {}", parent.display(), error)
            })?;
        }
        remove_sqlite_sidecar_files(&target_db_path)?;
        fs::copy(&backup_db_path, &target_db_path).map_err(|error| {
            format!(
                "恢复 SQLite 会话库失败 ({} -> {}): {}",
                backup_db_path.display(),
                target_db_path.display(),
                error
            )
        })?;
        remove_sqlite_sidecar_files(&target_db_path)?;
        restored.push(relative.to_string_lossy().replace('\\', "/"));
    }
    Ok(restored)
}

fn list_backup_sqlite_files(root: &Path) -> Result<Vec<PathBuf>, String> {
    let mut result = Vec::new();
    let entries = fs::read_dir(root)
        .map_err(|error| format!("读取 SQLite 备份目录失败 ({}): {}", root.display(), error))?;
    for entry in entries {
        let entry = entry.map_err(|error| {
            format!("读取 SQLite 备份目录项失败 ({}): {}", root.display(), error)
        })?;
        let path = entry.path();
        let file_type = entry.file_type().map_err(|error| {
            format!(
                "读取 SQLite 备份文件类型失败 ({}): {}",
                path.display(),
                error
            )
        })?;
        if file_type.is_symlink() {
            return Err(format!(
                "SQLite 备份包含不安全的符号链接: {}",
                path.display()
            ));
        }
        if file_type.is_dir() {
            result.extend(list_backup_sqlite_files(&path)?);
        } else if file_type.is_file() {
            result.push(path);
        } else {
            return Err(format!(
                "SQLite 备份包含不支持的文件类型: {}",
                path.display()
            ));
        }
    }
    result.sort();
    Ok(result)
}

fn backup_instance_files(
    data_dir: &Path,
    rollout_changes: &[RolloutProviderChange],
    include_sqlite: bool,
    include_session_index: bool,
    include_global_state: bool,
    instance_id: &str,
    target_provider: &str,
    options: CodexSessionVisibilityRepairOptions,
) -> Result<PathBuf, String> {
    let scope = modules::backup_storage::scope_for_path(data_dir);
    let operation_id = format!(
        "{}{}-{}{}",
        SESSION_VISIBILITY_REPAIR_BACKUP_PREFIX,
        Utc::now().format("%Y%m%d-%H%M%S"),
        uuid::Uuid::new_v4(),
        SESSION_VISIBILITY_REPAIR_BACKUP_SUFFIX
    );
    let backup_dir = modules::backup_storage::behavior_backup_dir("codex", &scope, &operation_id)?;
    let scope_dir = session_visibility_repair_backup_scope_dir(data_dir)?;
    validate_session_visibility_repair_backup_dir(&scope_dir, &backup_dir)?;

    let mut backed_up_files = Vec::new();
    let mut sqlite_backup_files = Vec::new();
    for change in rollout_changes {
        let target = backup_dir.join("files").join(&change.relative_path);
        if let Some(parent) = target.parent() {
            fs::create_dir_all(parent).map_err(|error| {
                format!(
                    "创建 rollout 备份目录失败 ({}): {}",
                    parent.display(),
                    error
                )
            })?;
        }
        fs::copy(&change.absolute_path, &target).map_err(|error| {
            format!(
                "备份 rollout 文件失败 ({} -> {}): {}",
                change.absolute_path.display(),
                target.display(),
                error
            )
        })?;
        modules::codex_session_file_time::restore_modified_time(
            &target,
            modules::codex_session_file_time::read_modified_time(&change.absolute_path),
        )?;
        backed_up_files.push(change.relative_path.to_string_lossy().to_string());
    }

    if include_sqlite {
        sqlite_backup_files = backup_sqlite_databases(data_dir, &backup_dir, options)?;
    }

    let mut session_index_backup_created = false;
    if include_session_index {
        let source = data_dir.join(SESSION_INDEX_FILE);
        if source.exists() {
            let target = backup_dir.join("files").join(SESSION_INDEX_FILE);
            if let Some(parent) = target.parent() {
                fs::create_dir_all(parent).map_err(|error| {
                    format!(
                        "创建 session_index 备份目录失败 ({}): {}",
                        parent.display(),
                        error
                    )
                })?;
            }
            fs::copy(&source, &target).map_err(|error| {
                format!(
                    "备份 session_index.jsonl 失败 ({} -> {}): {}",
                    source.display(),
                    target.display(),
                    error
                )
            })?;
            session_index_backup_created = true;
        }
    }

    let mut global_state_backup_created = false;
    if include_global_state {
        let source = data_dir.join(GLOBAL_STATE_FILE);
        if source.exists() {
            let target = backup_dir.join("files").join(GLOBAL_STATE_FILE);
            if let Some(parent) = target.parent() {
                fs::create_dir_all(parent).map_err(|error| {
                    format!("创建全局状态备份目录失败 ({}): {error}", parent.display())
                })?;
            }
            fs::copy(&source, &target).map_err(|error| {
                format!(
                    "备份 Codex 全局状态失败 ({} -> {}): {error}",
                    source.display(),
                    target.display()
                )
            })?;
            global_state_backup_created = true;
        }
    }

    let created_at = Utc::now().to_rfc3339();
    let manifest = SessionVisibilityRepairOperationManifest {
        version: SESSION_VISIBILITY_REPAIR_OPERATION_VERSION,
        instance_id: instance_id.to_string(),
        instance_root: data_dir.to_string_lossy().to_string(),
        target_provider: target_provider.to_string(),
        created_at: created_at.clone(),
        instance_scope: scope,
        operation_status: SessionVisibilityRepairOperationStatus::Prepared,
        operation_updated_at: created_at,
        operation_message: "备份已验证，等待开始写入".to_string(),
        has_sqlite_backup: !sqlite_backup_files.is_empty(),
        sqlite_files: sqlite_backup_files,
        has_session_index_backup: session_index_backup_created,
        has_global_state_backup: global_state_backup_created,
        rollout_files: backed_up_files,
    };
    write_session_visibility_repair_operation_manifest(&backup_dir, &manifest)?;

    Ok(backup_dir)
}

fn parse_session_visibility_repair_backup_timestamp(name: &str) -> Option<&str> {
    let operation_id = name
        .strip_prefix(SESSION_VISIBILITY_REPAIR_BACKUP_PREFIX)?
        .strip_suffix(SESSION_VISIBILITY_REPAIR_BACKUP_SUFFIX)?;
    // v1 backups used only the timestamp. v2 adds a UUID to prevent two
    // repairs started in the same second from sharing a directory. Keep both
    // layouts recognizable so retention follows the same rule as creation.
    let timestamp = operation_id.get(..15)?;
    let suffix = operation_id.get(15..)?;
    if !suffix.is_empty() && (suffix == "-" || !suffix.starts_with('-')) {
        return None;
    }
    if !timestamp.chars().enumerate().all(|(index, value)| {
        if index == 8 {
            value == '-'
        } else {
            value.is_ascii_digit()
        }
    }) {
        return None;
    }
    Some(timestamp)
}

fn prune_session_visibility_repair_backups(instances: &[CodexSyncInstance]) {
    for instance in instances {
        if let Err(error) = prune_instance_session_visibility_repair_backups(&instance.data_dir) {
            modules::logger::log_warn(&format!(
                "清理 Codex 会话可见性修复旧备份失败 ({}): {}",
                instance.data_dir.display(),
                error
            ));
        }
    }
}

fn prune_instance_session_visibility_repair_backups(data_dir: &Path) -> Result<(), String> {
    let scope_dir = session_visibility_repair_backup_scope_dir(data_dir)?;
    let entries = match fs::read_dir(&scope_dir) {
        Ok(entries) => entries,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(error) => {
            return Err(format!(
                "读取会话可见性修复备份目录失败 ({}): {}",
                scope_dir.display(),
                error
            ));
        }
    };
    let mut backups: Vec<(String, PathBuf)> = Vec::new();

    for entry in entries {
        let entry = entry
            .map_err(|error| format!("读取实例目录项失败 ({}): {}", data_dir.display(), error))?;
        let file_type = entry.file_type().map_err(|error| {
            format!(
                "读取实例目录项类型失败 ({}): {}",
                entry.path().display(),
                error
            )
        })?;
        if !file_type.is_dir() || file_type.is_symlink() {
            continue;
        }

        let file_name = entry.file_name();
        let Some(file_name) = file_name.to_str() else {
            continue;
        };
        let Some(timestamp) = parse_session_visibility_repair_backup_timestamp(file_name) else {
            continue;
        };

        // A prepared/applying journal is the only copy that can restore an
        // interrupted repair. Never remove it during routine retention.
        let manifest = match read_session_visibility_repair_operation_manifest(&scope_dir, &entry.path()) {
            Ok(manifest) => manifest,
            Err(_) => continue,
        };
        if matches!(
            manifest.operation_status,
            SessionVisibilityRepairOperationStatus::Prepared
                | SessionVisibilityRepairOperationStatus::Applying
        ) {
            continue;
        }
        backups.push((timestamp.to_string(), entry.path()));
    }

    if backups.len() <= MAX_SESSION_VISIBILITY_REPAIR_BACKUPS {
        return Ok(());
    }

    backups.sort_by(|left, right| right.0.cmp(&left.0));
    for (_, path) in backups
        .into_iter()
        .skip(MAX_SESSION_VISIBILITY_REPAIR_BACKUPS)
    {
        fs::remove_dir_all(&path)
            .map_err(|error| format!("删除旧备份失败 ({}): {}", path.display(), error))?;
    }

    Ok(())
}

fn restore_instance_files_from_backup(
    data_dir: &Path,
    backup_dir: &Path,
    include_sqlite: bool,
) -> Result<(), String> {
    let files_root = backup_dir.join("files");
    if files_root.exists() {
        restore_directory_contents(&files_root, data_dir)?;
    }

    if include_sqlite {
        let _ = restore_sqlite_databases_from_backup(data_dir, backup_dir)?;
    }

    Ok(())
}

fn restore_directory_contents(source_root: &Path, target_root: &Path) -> Result<(), String> {
    let entries = fs::read_dir(source_root)
        .map_err(|error| format!("读取备份目录失败 ({}): {}", source_root.display(), error))?;
    for entry in entries {
        let entry = entry.map_err(|error| {
            format!("读取备份目录项失败 ({}): {}", source_root.display(), error)
        })?;
        let source_path = entry.path();
        let file_type = entry.file_type().map_err(|error| {
            format!(
                "读取备份文件类型失败 ({}): {}",
                source_path.display(),
                error
            )
        })?;
        if file_type.is_symlink() {
            return Err(format!(
                "会话可见性修复备份包含不安全的符号链接: {}",
                source_path.display()
            ));
        }
        let relative = source_path
            .strip_prefix(source_root)
            .map_err(|_| format!("无法计算备份相对路径: {}", source_path.display()))?;
        let target_path = target_root.join(relative);

        if file_type.is_dir() {
            fs::create_dir_all(&target_path).map_err(|error| {
                format!("创建恢复目录失败 ({}): {}", target_path.display(), error)
            })?;
            restore_directory_contents(&source_path, &target_path)?;
            continue;
        }

        if !file_type.is_file() {
            return Err(format!(
                "会话可见性修复备份包含不支持的文件类型: {}",
                source_path.display()
            ));
        }

        if let Some(parent) = target_path.parent() {
            fs::create_dir_all(parent)
                .map_err(|error| format!("创建恢复父目录失败 ({}): {}", parent.display(), error))?;
        }
        fs::copy(&source_path, &target_path).map_err(|error| {
            format!(
                "恢复备份文件失败 ({} -> {}): {}",
                source_path.display(),
                target_path.display(),
                error
            )
        })?;
        modules::codex_session_file_time::restore_modified_time(
            &target_path,
            modules::codex_session_file_time::read_modified_time(&source_path),
        )?;
    }
    Ok(())
}
