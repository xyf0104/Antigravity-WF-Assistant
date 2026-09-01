use base64::Engine;
use rand::Rng;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::sync::{Arc, Mutex};

use crate::models::cursor::CursorImportPayload;
use crate::modules::logger;

const CURSOR_LOGIN_URL: &str = "https://cursor.com/loginDeepControl";
const CURSOR_POLL_ENDPOINT: &str = "https://api2.cursor.sh/auth/poll";
const OAUTH_POLL_INTERVAL_MS: u64 = 2000;
const OAUTH_MAX_POLLS: u32 = 150;
const OAUTH_TIMEOUT_SECONDS: i64 = 300;
const OAUTH_STATE_FILE: &str = "cursor_oauth_pending.json";
const OAUTH_TIMEOUT_ERROR: &str = "Cursor 登录轮询超时，请重试";

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CursorOAuthStartResponse {
    pub login_id: String,
    pub verification_uri: String,
    pub expires_in: u64,
    pub interval_seconds: u64,
}

/// The verifier and Cursor polling UUID are intentionally persisted only through
/// `oauth_pending_state`, which writes under the app-private data directory.
/// The verifier is never returned to the renderer or written to logs. Cursor's
/// poll UUID appears only in the browser URL required by Cursor's protocol.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct PendingOAuthState {
    login_id: String,
    uuid: String,
    code_verifier: String,
    expires_at: i64,
    #[serde(default)]
    cancelled: bool,
    /// Runtime-only guard. It prevents two renderer requests from consuming the
    /// same pending login at once, while still allowing a process restart to resume.
    #[serde(skip)]
    completion_claimed: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct PollResponse {
    access_token: Option<String>,
    refresh_token: Option<String>,
    auth_id: Option<String>,
}

lazy_static::lazy_static! {
    static ref PENDING_OAUTH_STATE: Arc<Mutex<Option<PendingOAuthState>>> = Arc::new(Mutex::new(None));
}

fn now_timestamp() -> i64 {
    chrono::Utc::now().timestamp()
}

fn generate_code_verifier() -> String {
    let mut rng = rand::thread_rng();
    let bytes: Vec<u8> = (0..32).map(|_| rng.gen::<u8>()).collect();
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(bytes)
}

fn generate_code_challenge(code_verifier: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(code_verifier.as_bytes());
    let digest = hasher.finalize();
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(digest)
}

fn generate_uuid() -> String {
    uuid::Uuid::new_v4().to_string()
}

fn remaining_seconds(state: &PendingOAuthState, now: i64) -> Option<u64> {
    if state.cancelled || state.expires_at <= now {
        return None;
    }
    Some((state.expires_at - now) as u64)
}

fn verification_uri(state: &PendingOAuthState) -> String {
    let code_challenge = generate_code_challenge(&state.code_verifier);
    format!(
        "{}?challenge={}&uuid={}&mode=login",
        CURSOR_LOGIN_URL, code_challenge, state.uuid
    )
}

fn to_start_response(state: &PendingOAuthState, now: i64) -> Option<CursorOAuthStartResponse> {
    let expires_in = remaining_seconds(state, now)?;
    Some(CursorOAuthStartResponse {
        login_id: state.login_id.clone(),
        verification_uri: verification_uri(state),
        expires_in,
        interval_seconds: OAUTH_POLL_INTERVAL_MS / 1000,
    })
}

fn persist_pending_login_locked(state: Option<&PendingOAuthState>) -> Result<(), String> {
    match state {
        Some(value) => crate::modules::oauth_pending_state::save(OAUTH_STATE_FILE, value),
        None => crate::modules::oauth_pending_state::clear(OAUTH_STATE_FILE),
    }
}

/// Update memory and durable state as one serialized operation. A new browser
/// authorization is not exposed until its PKCE state is safely persisted.
fn replace_pending_login(state: Option<PendingOAuthState>) -> Result<(), String> {
    let mut guard = PENDING_OAUTH_STATE
        .lock()
        .map_err(|_| "获取 Cursor OAuth 状态锁失败".to_string())?;
    persist_pending_login_locked(state.as_ref())?;
    *guard = state;
    Ok(())
}

fn clear_pending_login_locked(guard: &mut Option<PendingOAuthState>) -> Result<(), String> {
    persist_pending_login_locked(None)?;
    *guard = None;
    Ok(())
}

fn load_pending_login_from_disk() -> Option<PendingOAuthState> {
    match crate::modules::oauth_pending_state::load::<PendingOAuthState>(OAUTH_STATE_FILE) {
        Ok(Some(state)) => {
            if remaining_seconds(&state, now_timestamp()).is_none() {
                if crate::modules::oauth_pending_state::clear(OAUTH_STATE_FILE).is_err() {
                    logger::log_warn("[Cursor OAuth] 过期登录状态清理失败，已拒绝恢复该会话");
                }
                None
            } else {
                Some(state)
            }
        }
        Ok(None) => None,
        Err(_) => {
            logger::log_warn("[Cursor OAuth] 读取持久化登录状态失败，已拒绝恢复该会话");
            let _ = crate::modules::oauth_pending_state::clear(OAUTH_STATE_FILE);
            None
        }
    }
}

fn hydrate_pending_login_if_missing() {
    if let Ok(mut guard) = PENDING_OAUTH_STATE.lock() {
        if guard.is_none() {
            *guard = load_pending_login_from_disk();
        }
    }
}

fn claim_pending_login(login_id: &str) -> Result<PendingOAuthState, String> {
    hydrate_pending_login_if_missing();
    let mut guard = PENDING_OAUTH_STATE
        .lock()
        .map_err(|_| "获取 Cursor OAuth 状态锁失败".to_string())?;
    let Some(state) = guard.as_mut() else {
        return Err("没有进行中的 Cursor 登录会话".to_string());
    };

    if state.login_id != login_id {
        return Err("Cursor OAuth 登录会话已变更，请重新开始".to_string());
    }
    if state.cancelled {
        let _ = clear_pending_login_locked(&mut guard);
        return Err("Cursor OAuth 登录已取消".to_string());
    }
    if state.expires_at <= now_timestamp() {
        let _ = clear_pending_login_locked(&mut guard);
        return Err("Cursor OAuth 登录会话已过期，请重新开始".to_string());
    }
    if state.completion_claimed {
        return Err("Cursor OAuth 正在等待浏览器登录完成".to_string());
    }

    state.completion_claimed = true;
    Ok(state.clone())
}

fn validate_claimed_login(login_id: &str, expected_uuid: &str) -> Result<(), String> {
    hydrate_pending_login_if_missing();
    let mut guard = PENDING_OAUTH_STATE
        .lock()
        .map_err(|_| "获取 Cursor OAuth 状态锁失败".to_string())?;
    let Some(state) = guard.as_ref() else {
        return Err("Cursor OAuth 登录已取消或已被替换".to_string());
    };

    if state.login_id != login_id || state.uuid != expected_uuid {
        return Err("Cursor OAuth 登录会话已变更，请重新开始".to_string());
    }
    if state.cancelled {
        let _ = clear_pending_login_locked(&mut guard);
        return Err("Cursor OAuth 登录已取消".to_string());
    }
    if state.expires_at <= now_timestamp() {
        let _ = clear_pending_login_locked(&mut guard);
        return Err("Cursor OAuth 登录会话已过期，请重新开始".to_string());
    }
    Ok(())
}

fn release_completion_claim(login_id: &str, expected_uuid: &str) {
    if let Ok(mut guard) = PENDING_OAUTH_STATE.lock() {
        if let Some(state) = guard.as_mut() {
            if state.login_id == login_id && state.uuid == expected_uuid {
                state.completion_claimed = false;
            }
        }
    }
}

/// Consume exactly the pending login that supplied a successful poll response.
/// The durable state is removed before the result can be returned to the caller.
fn consume_pending_login(login_id: &str, expected_uuid: &str) -> Result<(), String> {
    let mut guard = PENDING_OAUTH_STATE
        .lock()
        .map_err(|_| "获取 Cursor OAuth 状态锁失败".to_string())?;
    let matches_active = guard
        .as_ref()
        .map(|state| state.login_id == login_id && state.uuid == expected_uuid)
        .unwrap_or(false);
    if !matches_active {
        return Err("Cursor OAuth 登录会话已变更，请重新开始".to_string());
    }
    clear_pending_login_locked(&mut guard)
        .map_err(|_| "无法安全清理 Cursor OAuth 登录会话，请重试".to_string())
}

fn poll_error_category(error: &reqwest::Error) -> &'static str {
    if error.is_timeout() {
        "timeout"
    } else if error.is_connect() {
        "connect"
    } else if error.is_request() {
        "request"
    } else {
        "other"
    }
}

fn payload_from_poll_response(poll_data: PollResponse) -> Option<CursorImportPayload> {
    let (access_token, refresh_token) = (poll_data.access_token?, poll_data.refresh_token?);
    let email = poll_data
        .auth_id
        .as_deref()
        .filter(|value| value.contains('@'))
        .unwrap_or("")
        .to_string();

    let mut auth_raw = serde_json::Map::new();
    auth_raw.insert(
        "accessToken".to_string(),
        serde_json::Value::String(access_token.clone()),
    );
    auth_raw.insert(
        "refreshToken".to_string(),
        serde_json::Value::String(refresh_token.clone()),
    );
    if let Some(ref auth_id) = poll_data.auth_id {
        auth_raw.insert(
            "authId".to_string(),
            serde_json::Value::String(auth_id.clone()),
        );
    }

    Some(CursorImportPayload {
        email,
        auth_id: poll_data.auth_id,
        name: None,
        access_token,
        refresh_token: Some(refresh_token),
        membership_type: None,
        subscription_status: None,
        sign_up_type: None,
        cursor_auth_raw: Some(serde_json::Value::Object(auth_raw)),
        cursor_usage_raw: None,
        status: None,
        status_reason: None,
    })
}

pub fn start_login() -> Result<CursorOAuthStartResponse, String> {
    let state = PendingOAuthState {
        // Keep the renderer-facing handle separate from Cursor's poll UUID.
        login_id: generate_uuid(),
        uuid: generate_uuid(),
        code_verifier: generate_code_verifier(),
        expires_at: now_timestamp() + OAUTH_TIMEOUT_SECONDS,
        cancelled: false,
        completion_claimed: false,
    };
    let response = to_start_response(&state, now_timestamp())
        .ok_or_else(|| "无法创建有效的 Cursor OAuth 登录会话".to_string())?;

    replace_pending_login(Some(state))?;
    logger::log_info("[Cursor OAuth] 登录会话已创建");
    Ok(response)
}

/// Returns only the renderer-safe fields required to resume a login after an
/// application restart. The PKCE verifier remains private; Cursor's poll UUID
/// appears only inside the browser URL required by the provider protocol.
pub fn peek_pending_login() -> Option<CursorOAuthStartResponse> {
    hydrate_pending_login_if_missing();
    let mut guard = PENDING_OAUTH_STATE.lock().ok()?;
    let state = guard.as_ref()?;
    if let Some(response) = to_start_response(state, now_timestamp()) {
        return Some(response);
    }

    let _ = clear_pending_login_locked(&mut guard);
    None
}

/// Startup hydration is intentionally passive: Cursor uses browser polling,
/// so there is no loopback listener to restart. The renderer adopts this state
/// through `peek_pending_login` only when the user reopens the OAuth flow.
pub fn restore_pending_oauth_login() {
    if peek_pending_login().is_some() {
        logger::log_info("[Cursor OAuth] 已恢复未完成的登录会话");
    }
}

pub async fn complete_login(login_id: &str) -> Result<CursorImportPayload, String> {
    let state = claim_pending_login(login_id)?;
    logger::log_info("[Cursor OAuth] 开始等待浏览器完成登录");

    let client = match reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(15))
        .build()
    {
        Ok(client) => client,
        Err(_) => {
            release_completion_claim(login_id, &state.uuid);
            return Err("创建 Cursor 登录网络客户端失败，请重试".to_string());
        }
    };

    // CURSOR_POLL_ENDPOINT uses HTTPS. This URL is deliberately never logged:
    // it contains the PKCE verifier required by Cursor's polling protocol.
    // lgtm[rs/cleartext-transmission] HTTPS endpoint; verifier is TLS-protected.
    let poll_url = format!(
        "{}?uuid={}&verifier={}",
        CURSOR_POLL_ENDPOINT, state.uuid, state.code_verifier
    );

    let result = async {
        for attempt in 0..OAUTH_MAX_POLLS {
            validate_claimed_login(login_id, &state.uuid)?;

            match client
                .get(&poll_url)
                .header("Accept", "application/json")
                .send()
                .await
            {
                Ok(resp) => {
                    let status = resp.status().as_u16();
                    if status == 404 {
                        if attempt % 15 == 0 {
                            logger::log_info(&format!(
                                "[Cursor OAuth] 轮询中，等待用户完成登录... (attempt={})",
                                attempt
                            ));
                        }
                        tokio::time::sleep(std::time::Duration::from_millis(
                            OAUTH_POLL_INTERVAL_MS,
                        ))
                        .await;
                        continue;
                    }

                    if status != 200 {
                        logger::log_warn(&format!("[Cursor OAuth] 轮询返回异常状态码: {}", status));
                        tokio::time::sleep(std::time::Duration::from_millis(
                            OAUTH_POLL_INTERVAL_MS,
                        ))
                        .await;
                        continue;
                    }

                    let body = resp
                        .text()
                        .await
                        .map_err(|_| "读取 Cursor 登录结果失败，请重试".to_string())?;
                    let poll_data = serde_json::from_str::<PollResponse>(&body)
                        .map_err(|_| "解析 Cursor 登录结果失败，请重试".to_string())?;
                    if let Some(payload) = payload_from_poll_response(poll_data) {
                        return Ok(payload);
                    }

                    logger::log_warn("[Cursor OAuth] 轮询响应未包含完整登录凭据");
                    tokio::time::sleep(std::time::Duration::from_millis(OAUTH_POLL_INTERVAL_MS))
                        .await;
                }
                Err(error) => {
                    logger::log_warn(&format!(
                        "[Cursor OAuth] 轮询请求暂时失败: kind={}, 将重试",
                        poll_error_category(&error)
                    ));
                    tokio::time::sleep(std::time::Duration::from_millis(
                        OAUTH_POLL_INTERVAL_MS * 2,
                    ))
                    .await;
                }
            }
        }

        Err(OAUTH_TIMEOUT_ERROR.to_string())
    }
    .await;

    match result {
        Ok(payload) => {
            if let Err(error) = consume_pending_login(login_id, &state.uuid) {
                // Do not return credentials until the one-time pending state is
                // removed, but allow an explicit retry if the local cleanup was
                // temporarily blocked by the operating system.
                release_completion_claim(login_id, &state.uuid);
                return Err(error);
            }
            logger::log_info("[Cursor OAuth] 登录成功");
            Ok(payload)
        }
        Err(error) => {
            if error == OAUTH_TIMEOUT_ERROR {
                if consume_pending_login(login_id, &state.uuid).is_err() {
                    release_completion_claim(login_id, &state.uuid);
                    logger::log_warn("[Cursor OAuth] 超时会话清理失败，等待用户显式重试或取消");
                }
            } else {
                release_completion_claim(login_id, &state.uuid);
            }
            Err(error)
        }
    }
}

pub fn cancel_login(login_id: Option<&str>) -> Result<(), String> {
    hydrate_pending_login_if_missing();
    let mut guard = PENDING_OAUTH_STATE
        .lock()
        .map_err(|_| "获取 Cursor OAuth 状态锁失败".to_string())?;
    let Some(state) = guard.as_ref() else {
        return Ok(());
    };

    if let Some(expected_login_id) = login_id {
        if state.login_id != expected_login_id {
            return Err("Cursor OAuth 登录会话已变更，取消失败".to_string());
        }
    }

    clear_pending_login_locked(&mut guard)
        .map_err(|_| "无法清理 Cursor OAuth 登录会话，请重试".to_string())?;
    logger::log_info("[Cursor OAuth] 登录会话已取消并清理");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsString;
    use std::fs;
    use std::path::PathBuf;

    lazy_static::lazy_static! {
        static ref TEST_LOCK: Mutex<()> = Mutex::new(());
    }

    struct TestDataDir {
        root: PathBuf,
        previous_test_data_dir: Option<OsString>,
        previous_data_dir: Option<OsString>,
    }

    impl TestDataDir {
        fn new(name: &str) -> Self {
            let root = std::env::temp_dir().join(format!(
                "xiass-cursor-oauth-test-{}-{}",
                name,
                uuid::Uuid::new_v4()
            ));
            fs::create_dir_all(&root).expect("create isolated OAuth data directory");
            let previous_test_data_dir = std::env::var_os("XIASS_TOOLS_TEST_DATA_DIR");
            let previous_data_dir = std::env::var_os("XIASS_TOOLS_DATA_DIR");
            std::env::set_var("XIASS_TOOLS_TEST_DATA_DIR", &root);
            std::env::set_var("XIASS_TOOLS_DATA_DIR", &root);
            Self {
                root,
                previous_test_data_dir,
                previous_data_dir,
            }
        }
    }

    impl Drop for TestDataDir {
        fn drop(&mut self) {
            match self.previous_test_data_dir.as_ref() {
                Some(value) => std::env::set_var("XIASS_TOOLS_TEST_DATA_DIR", value),
                None => std::env::remove_var("XIASS_TOOLS_TEST_DATA_DIR"),
            }
            match self.previous_data_dir.as_ref() {
                Some(value) => std::env::set_var("XIASS_TOOLS_DATA_DIR", value),
                None => std::env::remove_var("XIASS_TOOLS_DATA_DIR"),
            }
            let _ = fs::remove_dir_all(&self.root);
        }
    }

    fn test_state(expires_at: i64) -> PendingOAuthState {
        PendingOAuthState {
            login_id: "test-login-id".to_string(),
            uuid: "test-poll-uuid".to_string(),
            code_verifier: "test-pkce-verifier".to_string(),
            expires_at,
            cancelled: false,
            completion_claimed: false,
        }
    }

    fn clear_memory_for_test() {
        *PENDING_OAUTH_STATE.lock().expect("lock pending state") = None;
    }

    #[test]
    fn pending_login_survives_memory_reset_and_exposes_only_safe_resume_fields() {
        let _serial = TEST_LOCK.lock().expect("lock OAuth tests");
        let _data_dir = TestDataDir::new("resume");
        clear_memory_for_test();
        let state = test_state(now_timestamp() + 120);
        replace_pending_login(Some(state.clone())).expect("persist pending login");

        let stored: serde_json::Value = crate::modules::oauth_pending_state::load(OAUTH_STATE_FILE)
            .expect("read pending file")
            .expect("pending file present");
        assert_eq!(stored["codeVerifier"], state.code_verifier);

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let path = crate::modules::account::get_data_dir()
                .expect("data dir")
                .join("oauth_pending")
                .join(OAUTH_STATE_FILE);
            assert_eq!(
                fs::metadata(path).expect("metadata").permissions().mode() & 0o077,
                0
            );
        }

        clear_memory_for_test();
        restore_pending_oauth_login();
        let response = peek_pending_login().expect("resumed login");
        assert_eq!(response.login_id, state.login_id);
        assert!(response.expires_in > 0);
        assert!(response.verification_uri.contains("challenge="));
        let response_json = serde_json::to_value(response).expect("serialize response");
        assert!(response_json.get("codeVerifier").is_none());
        assert!(response_json.get("uuid").is_none());

        cancel_login(Some(&state.login_id)).expect("cancel persisted login");
        assert!(peek_pending_login().is_none());
        assert!(
            crate::modules::oauth_pending_state::load::<serde_json::Value>(OAUTH_STATE_FILE)
                .expect("read pending file after cancel")
                .is_none()
        );
        clear_memory_for_test();
    }

    #[test]
    fn expired_persisted_login_is_purged_instead_of_resumed() {
        let _serial = TEST_LOCK.lock().expect("lock OAuth tests");
        let _data_dir = TestDataDir::new("expired");
        clear_memory_for_test();
        replace_pending_login(Some(test_state(now_timestamp() - 1)))
            .expect("persist expired login");
        clear_memory_for_test();

        restore_pending_oauth_login();
        assert!(peek_pending_login().is_none());
        assert!(
            crate::modules::oauth_pending_state::load::<serde_json::Value>(OAUTH_STATE_FILE)
                .expect("read pending file after expiry")
                .is_none()
        );
        clear_memory_for_test();
    }

    #[test]
    fn only_one_completion_request_can_claim_a_pending_login() {
        let _serial = TEST_LOCK.lock().expect("lock OAuth tests");
        clear_memory_for_test();
        let state = test_state(now_timestamp() + 60);
        *PENDING_OAUTH_STATE.lock().expect("lock pending state") = Some(state.clone());

        let claimed = claim_pending_login(&state.login_id).expect("first completion claim");
        assert_eq!(claimed.uuid, state.uuid);
        assert!(claim_pending_login(&state.login_id)
            .expect_err("second completion must be rejected")
            .contains("正在等待"));

        release_completion_claim(&state.login_id, &state.uuid);
        assert!(claim_pending_login(&state.login_id).is_ok());
        clear_memory_for_test();
    }

    #[test]
    fn cancel_rejects_a_different_login_id_without_deleting_the_current_flow() {
        let _serial = TEST_LOCK.lock().expect("lock OAuth tests");
        clear_memory_for_test();
        let state = test_state(now_timestamp() + 60);
        *PENDING_OAUTH_STATE.lock().expect("lock pending state") = Some(state.clone());

        assert!(cancel_login(Some("another-login-id")).is_err());
        assert_eq!(
            peek_pending_login()
                .expect("pending login remains")
                .login_id,
            state.login_id
        );
        clear_memory_for_test();
    }
}
