// Codex 账号模块：Codex home paths, storage migration and mutation/refresh locks。
// 通过 include! 保持原 modules::codex_account 作用域，完整保留私有调用关系。
const DATA_DIR_OVERRIDE_ENV_VARS: [&str; 3] = [
    "XIASS_TOOLS_TEST_DATA_DIR",
    "COCKPIT_TEST_DATA_DIR",
    "XIASS_TOOLS_DATA_DIR",
];

pub(crate) fn client_instance_id_for_profile_dir(base_dir: &Path) -> String {
    base_dir
        .file_name()
        .and_then(|name| name.to_str())
        .map(str::trim)
        .filter(|name| !name.is_empty())
        .unwrap_or("unknown")
        .to_string()
}

/// Cockpit/早期 XIASS 的只读迁移来源。旧目录只作为输入，迁移成功后也不删除，
/// 方便用户继续回退旧版本；所有新写入仍统一进入当前 `.xiass_tools` 数据目录。
fn legacy_codex_data_dirs() -> Vec<PathBuf> {
    let mut candidates = Vec::new();
    for data_dir in [dirs::data_dir(), dirs::data_local_dir()]
        .into_iter()
        .flatten()
    {
        for name in [
            "com.jlcodes.cockpit-tools",
            "com.jlcodes.xiass-tools",
            "com.antigravity.cockpit-tools",
            "com.antigravity.xiass-tools",
        ] {
            candidates.push(data_dir.join(name));
        }
    }
    if let Some(home) = dirs::home_dir() {
        let profile_dir = if account::is_dev_profile() {
            ".antigravity_cockpit_dev"
        } else {
            ".antigravity_cockpit"
        };
        candidates.push(home.join(profile_dir));
        candidates.push(home.join(".cockpit-tools"));
    }
    candidates.sort();
    candidates.dedup();
    candidates
}

fn copy_legacy_codex_data_if_needed(old_dir: &Path, new_data_dir: &Path) {
    if !old_dir.is_dir() || old_dir == new_data_dir {
        return;
    }
    let old_index = old_dir.join("codex_accounts.json");
    let new_index = new_data_dir.join("codex_accounts.json");
    // 只向尚未建立 Codex 索引的新目录导入一个完整历史数据集。若 XIASS
    // 已有账号库，则不混入另一把加密密钥或孤立账号详情。
    if !old_index.is_file() || new_index.exists() {
        return;
    }
    if let Err(error) = fs::create_dir_all(new_data_dir) {
        logger::log_warn(&format!(
            "[Codex Migration] 无法创建 XIASS 数据目录: path={}, error={}",
            new_data_dir.display(),
            error
        ));
        return;
    }

    // 账号详情可能使用旧数据目录里的本机加密密钥。只在 XIASS 目录尚无
    // 对应密钥时复制，绝不覆盖已经投入使用的新密钥。
    for file_name in ["secure-account-storage.key", "account-token.key"] {
        let source = old_dir.join(file_name);
        let destination = new_data_dir.join(file_name);
        if source.is_file() && !destination.exists() {
            if let Err(error) = fs::copy(&source, &destination) {
                logger::log_warn(&format!(
                    "[Codex Migration] 复制历史本机存储密钥失败: source={}, error={}",
                    old_dir.display(),
                    error
                ));
            }
        }
    }

    match fs::copy(&old_index, &new_index) {
        Ok(_) => {
            logger::log_info(&format!(
                "[Codex Migration] 已只读复制历史 codex_accounts.json: source={}",
                old_dir.display()
            ));
        }
        Err(e) => {
            logger::log_warn(&format!(
                "[Codex Migration] 复制历史 codex_accounts.json 失败: source={}, error={}",
                old_dir.display(), e
            ));
            return;
        }
    }

    // 迁移 codex_accounts/ 目录
    let old_accounts_dir = old_dir.join("codex_accounts");
    let new_accounts_dir = new_data_dir.join("codex_accounts");
    if old_accounts_dir.exists() && old_accounts_dir.is_dir() {
        if let Err(error) = fs::create_dir_all(&new_accounts_dir) {
            logger::log_warn(&format!(
                "[Codex Migration] 无法创建 XIASS 账号详情目录: path={}, error={}",
                new_accounts_dir.display(),
                error
            ));
            return;
        }
        if let Ok(entries) = fs::read_dir(&old_accounts_dir) {
            for entry in entries.flatten() {
                let old_path = entry.path();
                if !old_path.is_file() {
                    continue;
                }
                if let Some(fname) = old_path.file_name() {
                    let new_path = new_accounts_dir.join(fname);
                    if new_path.exists() {
                        // 新目录已有同名文件，跳过（不覆盖）
                        continue;
                    }
                    match fs::copy(&old_path, &new_path) {
                        Ok(_) => {
                            logger::log_info(&format!(
                                "[Codex Migration] 已只读复制历史账号文件: {:?}",
                                fname
                            ));
                        }
                        Err(e) => {
                            logger::log_warn(&format!(
                                "[Codex Migration] 账号文件迁移失败: {:?}, error={}",
                                fname, e
                            ));
                        }
                    }
                }
            }
        }
    }
}

fn migrate_codex_data_if_needed(new_data_dir: &Path) {
    for old_dir in legacy_codex_data_dirs() {
        copy_legacy_codex_data_if_needed(&old_dir, new_data_dir);
    }
}

fn has_explicit_data_dir_override<'a>(
    values: impl IntoIterator<Item = Option<&'a str>>,
) -> bool {
    values
        .into_iter()
        .flatten()
        .any(|value| !value.trim().is_empty())
}

fn should_migrate_legacy_codex_data_from_values<'a>(
    values: impl IntoIterator<Item = Option<&'a str>>,
) -> bool {
    !has_explicit_data_dir_override(values)
}

/// An explicit data-directory override creates an isolated profile. It must not
/// silently import account data or its encryption keys from a user's normal
/// profile, which is particularly important for installer/runtime smoke tests.
fn should_migrate_legacy_codex_data() -> bool {
    let values: Vec<Option<String>> = DATA_DIR_OVERRIDE_ENV_VARS
        .iter()
        .map(|key| std::env::var(key).ok())
        .collect();
    should_migrate_legacy_codex_data_from_values(values.iter().map(|value| value.as_deref()))
}

/// 获取我们的多账号存储路径（统一使用 ~/.xiass_tools/）
fn get_accounts_storage_path() -> PathBuf {
    let data_dir = account::get_data_dir().unwrap_or_else(|_| {
        dirs::home_dir()
            .expect("无法获取用户目录")
            .join(".xiass_tools")
    });
    fs::create_dir_all(&data_dir).ok();
    if should_migrate_legacy_codex_data() {
        migrate_codex_data_if_needed(&data_dir);
    }
    data_dir.join("codex_accounts.json")
}

/// 获取账号详情存储目录（统一使用 ~/.xiass_tools/codex_accounts/）
fn get_accounts_dir() -> PathBuf {
    let data_dir = account::get_data_dir().unwrap_or_else(|_| {
        dirs::home_dir()
            .expect("无法获取用户目录")
            .join(".xiass_tools")
    });
    let accounts_dir = data_dir.join("codex_accounts");
    fs::create_dir_all(&accounts_dir).ok();
    accounts_dir
}

fn account_tombstone_path(account_id: &str) -> PathBuf {
    let data_dir = account::get_data_dir().unwrap_or_else(|_| {
        dirs::home_dir()
            .expect("无法获取用户目录")
            .join(".xiass_tools")
    });
    data_dir
        .join(CODEX_ACCOUNT_TOMBSTONES_DIR)
        .join(format!("{}.json", account_id))
}

#[derive(Debug, serde::Deserialize, serde::Serialize)]
struct CodexAccountTombstone {
    deleted: bool,
    generation: u64,
    #[serde(default)]
    credential_hash: String,
}

fn account_credential_hash(account: &CodexAccount) -> String {
    let value = serde_json::json!({
        "auth_mode": &account.auth_mode,
        "tokens": &account.tokens,
        "openai_api_key": &account.openai_api_key,
        "agent_identity": &account.agent_identity,
    });
    URL_SAFE_NO_PAD.encode(Sha256::digest(value.to_string().as_bytes()))
}

fn read_account_tombstone(account_id: &str) -> Option<CodexAccountTombstone> {
    let path = account_tombstone_path(account_id);
    serde_json::from_str(&fs::read_to_string(path).ok()?).ok()
}

fn account_is_tombstoned(account_id: &str) -> bool {
    read_account_tombstone(account_id).is_some_and(|tombstone| tombstone.deleted)
}

fn validate_loaded_account_tombstone(account: &CodexAccount) -> Result<bool, String> {
    let Some(tombstone) = read_account_tombstone(&account.id) else {
        return Ok(true);
    };
    if tombstone.deleted {
        return Ok(false);
    }

    let credential_hash = account_credential_hash(account);
    if account.token_generation < tombstone.generation
        || (account.token_generation == tombstone.generation
            && !tombstone.credential_hash.is_empty()
            && credential_hash != tombstone.credential_hash)
    {
        return Err(format!(
            "账号详情中的凭据快照已过期，拒绝加载: account_id={}",
            account.id
        ));
    }

    Ok(true)
}

fn write_account_tombstone(
    account_id: &str,
    deleted: bool,
    generation: u64,
    credential_hash: String,
) -> Result<(), String> {
    let path = account_tombstone_path(account_id);
    let content = CodexAccountTombstone {
        deleted,
        generation,
        credential_hash,
    };
    let serialized = serde_json::to_string(&content)
        .map_err(|error| format!("序列化账号删除标记失败: {}", error))?;
    crate::modules::atomic_write::write_string_atomic(&path, &serialized)
        .map_err(|error| format!("写入账号删除标记失败: {}", error))
}

/// 解析 JWT Token 的 payload
pub fn decode_jwt_payload(token: &str) -> Result<CodexJwtPayload, String> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() < 2 {
        return Err("无效的 JWT Token 格式".to_string());
    }

    let payload_b64 = parts[1];
    let payload_bytes = URL_SAFE_NO_PAD
        .decode(payload_b64)
        .map_err(|e| format!("Base64 解码失败: {}", e))?;

    let payload: CodexJwtPayload =
        serde_json::from_slice(&payload_bytes).map_err(|e| format!("JSON 解析失败: {}", e))?;

    Ok(payload)
}

fn decode_jwt_payload_value(token: &str) -> Option<serde_json::Value> {
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() != 3 {
        return None;
    }

    let payload_bytes = URL_SAFE_NO_PAD.decode(parts[1]).ok()?;
    let payload_str = String::from_utf8(payload_bytes).ok()?;
    serde_json::from_str(&payload_str).ok()
}

fn normalize_optional_value(value: Option<String>) -> Option<String> {
    value.and_then(|raw| {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed.to_string())
        }
    })
}

fn normalize_optional_ref(value: Option<&str>) -> Option<String> {
    value.and_then(|raw| {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed.to_string())
        }
    })
}

fn first_json_string(value: &serde_json::Value, paths: &[&[&str]]) -> Option<String> {
    paths.iter().find_map(|path| {
        let mut current = value;
        for key in *path {
            current = current.get(*key)?;
        }
        current
            .as_str()
            .and_then(|raw| normalize_optional_ref(Some(raw)))
    })
}

fn now_timestamp() -> i64 {
    chrono::Utc::now().timestamp()
}

fn codex_token_lock_for(account_id: &str) -> Arc<tokio::sync::Mutex<()>> {
    let mut locks = CODEX_TOKEN_REFRESH_LOCKS
        .lock()
        .unwrap_or_else(|err| err.into_inner());
    locks
        .entry(account_id.to_string())
        .or_insert_with(|| Arc::new(tokio::sync::Mutex::new(())))
        .clone()
}

fn loaded_account_token_generation(account_id: &str) -> Option<u64> {
    load_account(account_id).map(|account| account.token_generation)
}

struct CodexTokenRefreshFileLock {
    path: PathBuf,
}

impl Drop for CodexTokenRefreshFileLock {
    fn drop(&mut self) {
        if let Err(err) = fs::remove_dir_all(&self.path) {
            if err.kind() != std::io::ErrorKind::NotFound {
                logger::log_warn(&format!(
                    "释放 Codex Token 跨进程刷新锁失败: lock_path={}, error={}",
                    self.path.display(),
                    err
                ));
            }
        }
    }
}

/// 跨 dev/正式版进程协调官方 Codex profile 的写入租约。
///
/// 两个 XIASS Tools 环境共享默认 `~/.codex`，但各自的账号库和进程内锁彼此不可见。
/// 所有会改变默认 profile 凭据的完整事务都必须持有这把锁，避免“检查通过后被另一进程
/// 在启动前覆盖”的竞态。
pub(crate) struct CodexProfileMutationLease {
    path: PathBuf,
}

impl Drop for CodexProfileMutationLease {
    fn drop(&mut self) {
        if let Err(err) = fs::remove_dir_all(&self.path) {
            if err.kind() != std::io::ErrorKind::NotFound {
                logger::log_warn(&format!(
                    "释放 Codex profile 跨进程写入锁失败: lock_path={}, error={}",
                    self.path.display(),
                    err
                ));
            }
        }
    }
}

fn codex_account_lock_name(account_id: &str) -> String {
    let lock_identity = load_account(account_id)
        .and_then(|account| {
            normalize_optional_ref(account.account_id.as_deref())
                .map(|value| format!("chatgpt:{}", value))
                .or_else(|| {
                    normalize_optional_ref(Some(account.email.as_str()))
                        .map(|value| format!("email:{}", value.to_ascii_lowercase()))
                })
        })
        .unwrap_or_else(|| format!("local:{}", account_id));
    sha256_hex_bytes(lock_identity.as_bytes())
}

fn codex_token_refresh_file_lock_path(account_id: &str) -> PathBuf {
    // dev 与正式版使用独立账号库，因此也必须使用独立的 refresh_token 锁。
    // 这样两个环境可以分别复现各自的 RT 轮换行为，不会互相改变测试状态。
    // 优先使用服务端 ChatGPT account_id，让不同安装里可能不同的本地存储 ID
    // 仍映射到同一把锁；旧账号缺少该字段时再回退邮箱或本地 ID。
    let lock_name = codex_account_lock_name(account_id);
    let data_root = account::resolve_data_dir().unwrap_or_else(|_| {
        dirs::home_dir()
            .expect("无法获取用户目录")
            .join(".xiass_tools")
    });
    data_root
        .join(".cockpit-token-locks")
        .join(format!("token-refresh-{}.lock", lock_name))
}

fn codex_profile_mutation_lock_path(profile_dir: &Path) -> PathBuf {
    let normalized = profile_dir
        .to_string_lossy()
        .replace('\\', "/")
        .trim_end_matches('/')
        .to_ascii_lowercase();
    let lock_name = sha256_hex_bytes(normalized.as_bytes());
    let shared_root = codex_profile_mutation_lock_root().join(CODEX_PROFILE_MUTATION_LOCK_DIR);
    shared_root.join(format!("profile-{}.lock", lock_name))
}

fn codex_profile_mutation_lock_root() -> PathBuf {
    // Unit tests intentionally change HOME to isolate account stores and run in parallel.
    // Keep the cross-process profile lease on one stable test root; production continues to
    // share ~/.codex between dev and installed XIASS Tools environments.
    #[cfg(test)]
    {
        return std::env::temp_dir().join("cockpit-profile-mutation-lock-root");
    }

    #[cfg(not(test))]
    {
        dirs::home_dir()
            .map(|home| home.join(".codex"))
            .unwrap_or_else(get_codex_home)
    }
}

/// 用户主动触发的 profile 变更不能排队到另一环境操作完成后再执行。
/// 否则后到的环境会立即关闭前一个环境刚启动的官方客户端，造成“切进去又退出”。
pub(crate) fn try_acquire_profile_mutation_lease(
    profile_dir: &Path,
    reason: &str,
) -> Result<CodexProfileMutationLease, String> {
    let path = codex_profile_mutation_lock_path(profile_dir);
    let parent = path
        .parent()
        .ok_or_else(|| format!("Codex profile 写入锁路径无效: {}", path.display()))?;
    fs::create_dir_all(parent)
        .map_err(|err| format_io_error("创建 Codex profile 写入锁目录", parent, &err))?;

    for _ in 0..2 {
        match fs::create_dir(&path) {
            Ok(()) => {
                let owner = format!(
                    "pid={}\nprofile_dir={}\nprofile={}\nreason={}\ncreated_at={}\n",
                    std::process::id(),
                    profile_dir.display(),
                    std::env::var("XIASS_TOOLS_PROFILE").unwrap_or_else(|_| "prod".to_string()),
                    reason,
                    now_timestamp()
                );
                if let Err(error) = fs::write(path.join("owner"), owner) {
                    logger::log_warn(&format!(
                        "写入 Codex profile 写入锁元数据失败: lock_path={}, error={}",
                        path.display(),
                        error
                    ));
                }
                return Ok(CodexProfileMutationLease { path });
            }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                if codex_profile_mutation_lock_is_stale(&path) {
                    logger::log_warn(&format!(
                        "清理失效 Codex profile 写入锁: profile_dir={}, lock_path={}, reason={}",
                        profile_dir.display(),
                        path.display(),
                        reason
                    ));
                    let _ = fs::remove_dir_all(&path);
                    continue;
                }
                return Err(format!(
                    "另一个 XIASS Tools 环境正在操作同一个 Codex profile，请等待该操作完成后重试: profile_dir={}",
                    profile_dir.display()
                ));
            }
            Err(error) => {
                return Err(format_io_error("创建 Codex profile 写入锁", &path, &error));
            }
        }
    }

    Err(format!(
        "另一个 XIASS Tools 环境正在操作同一个 Codex profile，请稍后重试: profile_dir={}",
        profile_dir.display()
    ))
}

fn codex_profile_mutation_lock_owner_pid(path: &Path) -> Option<u32> {
    fs::read_to_string(path.join("owner"))
        .ok()
        .and_then(|content| {
            content.lines().find_map(|line| {
                line.strip_prefix("pid=")
                    .and_then(|value| value.trim().parse::<u32>().ok())
            })
        })
}

fn codex_profile_mutation_lock_is_stale(path: &Path) -> bool {
    match codex_profile_mutation_lock_owner_pid(path) {
        Some(pid) => !crate::modules::process::is_pid_running(pid),
        None => codex_token_refresh_file_lock_is_stale(path),
    }
}

pub(crate) fn profile_mutation_lease_held_by_other_process(profile_dir: &Path) -> bool {
    let path = codex_profile_mutation_lock_path(profile_dir);
    if !path.exists() {
        return false;
    }
    if codex_profile_mutation_lock_is_stale(&path) {
        let _ = fs::remove_dir_all(&path);
        return false;
    }
    let owner_pid = codex_profile_mutation_lock_owner_pid(&path);
    owner_pid != Some(std::process::id())
}

fn codex_token_refresh_file_lock_is_stale(path: &Path) -> bool {
    let Ok(metadata) = fs::metadata(path) else {
        return false;
    };
    let Ok(modified) = metadata.modified() else {
        return false;
    };
    SystemTime::now()
        .duration_since(modified)
        .map(|age| age >= Duration::from_secs(CODEX_TOKEN_REFRESH_FILE_LOCK_STALE_SECONDS))
        .unwrap_or(false)
}

async fn acquire_codex_token_refresh_file_lock(
    account_id: &str,
    reason: &str,
) -> Result<CodexTokenRefreshFileLock, String> {
    let path = codex_token_refresh_file_lock_path(account_id);
    let parent = path
        .parent()
        .ok_or_else(|| format!("Codex Token 刷新锁路径无效: {}", path.display()))?;
    fs::create_dir_all(parent)
        .map_err(|err| format_io_error("创建 Codex Token 刷新锁目录", parent, &err))?;

    let started = Instant::now();
    loop {
        match fs::create_dir(&path) {
            Ok(()) => {
                let owner_path = path.join("owner");
                let owner = format!(
                    "pid={}\naccount_id={}\nreason={}\ncreated_at={}\n",
                    std::process::id(),
                    account_id,
                    reason,
                    now_timestamp()
                );
                if let Err(err) = fs::write(&owner_path, owner) {
                    logger::log_warn(&format!(
                        "写入 Codex Token 跨进程刷新锁元数据失败: account_id={}, lock_path={}, error={}",
                        account_id,
                        owner_path.display(),
                        err
                    ));
                }
                return Ok(CodexTokenRefreshFileLock { path });
            }
            Err(err) if err.kind() == std::io::ErrorKind::AlreadyExists => {
                if codex_token_refresh_file_lock_is_stale(&path) {
                    logger::log_warn(&format!(
                        "清理过期 Codex Token 跨进程刷新锁: account_id={}, lock_path={}",
                        account_id,
                        path.display()
                    ));
                    if let Err(remove_err) = fs::remove_dir_all(&path) {
                        logger::log_warn(&format!(
                            "清理过期 Codex Token 跨进程刷新锁失败: account_id={}, lock_path={}, error={}",
                            account_id,
                            path.display(),
                            remove_err
                        ));
                    }
                    continue;
                }

                if started.elapsed()
                    >= Duration::from_secs(CODEX_TOKEN_REFRESH_FILE_LOCK_TIMEOUT_SECONDS)
                {
                    return Err(format!(
                        "等待 Codex Token 刷新锁超时: account_id={}, lock_path={}, reason={}",
                        account_id,
                        path.display(),
                        reason
                    ));
                }

                tokio::time::sleep(Duration::from_millis(CODEX_TOKEN_REFRESH_FILE_LOCK_POLL_MS))
                    .await;
            }
            Err(err) => {
                return Err(format_io_error("创建 Codex Token 刷新锁", &path, &err));
            }
        }
    }
}

fn mark_token_chain_updated(account: &mut CodexAccount) {
    account.token_generation = account.token_generation.saturating_add(1);
    account.token_updated_at = Some(now_timestamp());
    account.token_source_mode = CODEX_TOKEN_SOURCE_MANAGED.to_string();
    account.requires_reauth = false;
    account.reauth_reason = None;
}

fn sync_identity_from_tokens(account: &mut CodexAccount) {
    if let Ok((
        email,
        user_id,
        plan_type,
        subscription_active_until,
        id_token_account_id,
        id_token_org_id,
    )) = extract_user_info(&account.tokens.id_token)
    {
        if !email.trim().is_empty() {
            account.email = email;
        }
        account.user_id = user_id;
        account.plan_type = plan_type;
        account.subscription_active_until = subscription_active_until;
        account.account_id = normalize_optional_value(
            extract_chatgpt_account_id_from_access_token(&account.tokens.access_token)
                .or(id_token_account_id)
                .or_else(|| account.account_id.clone()),
        );
        account.organization_id = normalize_optional_value(
            extract_chatgpt_organization_id_from_access_token(&account.tokens.access_token)
                .or(id_token_org_id)
                .or_else(|| account.organization_id.clone()),
        );
    }
}

#[cfg(test)]
mod xiass_legacy_storage_migration_tests {
    use super::{
        copy_legacy_codex_data_if_needed, has_explicit_data_dir_override,
        should_migrate_legacy_codex_data_from_values, DATA_DIR_OVERRIDE_ENV_VARS,
    };
    use std::fs;

    #[test]
    fn explicit_data_directory_values_disable_legacy_migration() {
        assert_eq!(
            DATA_DIR_OVERRIDE_ENV_VARS,
            [
                "XIASS_TOOLS_TEST_DATA_DIR",
                "COCKPIT_TEST_DATA_DIR",
                "XIASS_TOOLS_DATA_DIR",
            ]
        );
        assert!(has_explicit_data_dir_override([
            None,
            Some(" /private/tmp/xiass-smoke "),
            None,
        ]));
        assert!(!has_explicit_data_dir_override([
            None,
            Some("   "),
            None,
        ]));
        assert!(should_migrate_legacy_codex_data_from_values([
            None,
            Some("   "),
            None,
        ]));
        assert!(!should_migrate_legacy_codex_data_from_values([
            None,
            Some(" /private/tmp/xiass-smoke "),
            None,
        ]));
    }

    #[test]
    fn legacy_storage_is_copied_without_deleting_the_source() {
        let root = std::env::temp_dir().join(format!(
            "xiass-legacy-codex-migration-{}",
            uuid::Uuid::new_v4()
        ));
        let source = root.join("legacy");
        let destination = root.join("xiass");
        fs::create_dir_all(source.join("codex_accounts")).expect("create legacy data");
        fs::write(source.join("codex_accounts.json"), "{\"accounts\":[]}")
            .expect("write legacy index");
        fs::write(source.join("codex_accounts/account.json"), "{}")
            .expect("write legacy account");
        fs::write(source.join("secure-account-storage.key"), "legacy-key")
            .expect("write legacy key");

        copy_legacy_codex_data_if_needed(&source, &destination);

        assert!(source.join("codex_accounts.json").is_file());
        assert!(source.join("codex_accounts/account.json").is_file());
        assert!(source.join("secure-account-storage.key").is_file());
        assert_eq!(
            fs::read_to_string(destination.join("codex_accounts.json"))
                .expect("read migrated index"),
            "{\"accounts\":[]}"
        );
        assert!(destination.join("codex_accounts/account.json").is_file());
        assert_eq!(
            fs::read_to_string(destination.join("secure-account-storage.key"))
                .expect("read migrated key"),
            "legacy-key"
        );
        let _ = fs::remove_dir_all(root);
    }
}
