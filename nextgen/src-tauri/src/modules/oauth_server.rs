use crate::modules::oauth;
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use rand::Rng;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::sync::{Arc, Mutex, OnceLock};
use tauri::Url;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio::sync::watch;
use tokio::time::{timeout, Duration};

const OAUTH_CALLBACK_PATH: &str = "/oauth-callback";
const OAUTH_PENDING_STATE_FILE: &str = "antigravity_oauth_pending.json";
const MAX_HTTP_REQUEST_BYTES: usize = 32 * 1024;
const REQUEST_READ_TIMEOUT: Duration = Duration::from_secs(5);
const OAUTH_FLOW_TIMEOUT_SECONDS: i64 = 10 * 60;
const OAUTH_FLOW_WAIT_TIMEOUT: Duration = Duration::from_secs(10 * 60);
const MAX_PENDING_CLOCK_SKEW_SECONDS: i64 = 60;

#[derive(Clone, Serialize, Deserialize)]
struct PendingOAuthFlowState {
    redirect_uri: String,
    expected_state: String,
    code_verifier: String,
    created_at: i64,
    expires_at: i64,
}

struct OAuthFlowState {
    auth_url: String,
    pending: PendingOAuthFlowState,
    cancel_tx: watch::Sender<bool>,
    code_tx: Arc<tokio::sync::Mutex<Option<oneshot::Sender<Result<String, String>>>>>,
    code_rx: Option<oneshot::Receiver<Result<String, String>>>,
}

static OAUTH_FLOW_STATE: OnceLock<Mutex<Option<OAuthFlowState>>> = OnceLock::new();

fn get_oauth_flow_state() -> &'static Mutex<Option<OAuthFlowState>> {
    OAUTH_FLOW_STATE.get_or_init(|| Mutex::new(None))
}

fn now_timestamp() -> i64 {
    chrono::Utc::now().timestamp()
}

fn generate_base64url_token(byte_count: usize) -> String {
    let mut rng = rand::thread_rng();
    let bytes: Vec<u8> = (0..byte_count).map(|_| rng.gen::<u8>()).collect();
    URL_SAFE_NO_PAD.encode(bytes)
}

fn pkce_code_challenge(code_verifier: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(code_verifier.as_bytes());
    URL_SAFE_NO_PAD.encode(hasher.finalize())
}

fn is_valid_base64url_token(value: &str, min_len: usize, max_len: usize) -> bool {
    let value = value.trim();
    value.len() >= min_len
        && value.len() <= max_len
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
}

fn parse_valid_loopback_redirect_uri(redirect_uri: &str) -> Result<(Url, u16), String> {
    let redirect = Url::parse(redirect_uri).map_err(|_| "OAuth redirect_uri 无效".to_string())?;
    if redirect.scheme() != "http"
        || !redirect.username().is_empty()
        || redirect.password().is_some()
        || redirect.query().is_some()
        || redirect.fragment().is_some()
        || redirect.path() != OAUTH_CALLBACK_PATH
    {
        return Err("OAuth redirect_uri 必须是本机回调地址".to_string());
    }

    let host = redirect
        .host_str()
        .ok_or_else(|| "OAuth redirect_uri 缺少主机".to_string())?;
    if !(host.eq_ignore_ascii_case("localhost") || host == "127.0.0.1") {
        return Err("OAuth redirect_uri 仅允许 localhost 或 127.0.0.1".to_string());
    }

    let port = redirect
        .port()
        .filter(|port| *port > 0)
        .ok_or_else(|| "OAuth redirect_uri 缺少本地回调端口".to_string())?;
    Ok((redirect, port))
}

fn validate_pending_oauth_state_at(
    pending: &PendingOAuthFlowState,
    now: i64,
) -> Result<u16, String> {
    if pending.created_at <= 0
        || pending.expires_at <= pending.created_at
        || pending.created_at > now + MAX_PENDING_CLOCK_SKEW_SECONDS
        || pending.expires_at - pending.created_at > OAUTH_FLOW_TIMEOUT_SECONDS
        || pending.expires_at <= now
    {
        return Err("OAuth 登录会话已过期或时间戳无效".to_string());
    }
    if !is_valid_base64url_token(&pending.expected_state, 43, 128) {
        return Err("OAuth 登录会话 state 无效".to_string());
    }
    if !is_valid_base64url_token(&pending.code_verifier, 43, 128) {
        return Err("OAuth 登录会话 PKCE verifier 无效".to_string());
    }
    let (_, port) = parse_valid_loopback_redirect_uri(&pending.redirect_uri)?;
    Ok(port)
}

fn validate_pending_oauth_state(pending: &PendingOAuthFlowState) -> Result<u16, String> {
    validate_pending_oauth_state_at(pending, now_timestamp())
}

fn oauth_auth_url_for_pending(pending: &PendingOAuthFlowState) -> String {
    let code_challenge = pkce_code_challenge(&pending.code_verifier);
    oauth::get_auth_url(
        pending.redirect_uri.as_str(),
        pending.expected_state.as_str(),
        code_challenge.as_str(),
    )
}

fn persist_pending_oauth_state(pending: Option<&PendingOAuthFlowState>) -> Result<(), String> {
    let result = match pending {
        Some(value) => crate::modules::oauth_pending_state::save(OAUTH_PENDING_STATE_FILE, value),
        None => crate::modules::oauth_pending_state::clear(OAUTH_PENDING_STATE_FILE),
    };
    result.map_err(|error| format!("持久化 Antigravity OAuth 登录会话失败: {}", error))
}

fn clear_persisted_oauth_state_if_matches(expected_state: &str) -> Result<(), String> {
    let pending = crate::modules::oauth_pending_state::load::<PendingOAuthFlowState>(
        OAUTH_PENDING_STATE_FILE,
    )?;
    if pending
        .as_ref()
        .is_some_and(|current| current.expected_state == expected_state)
    {
        persist_pending_oauth_state(None)?;
    }
    Ok(())
}

fn load_pending_oauth_state_from_disk() -> Option<PendingOAuthFlowState> {
    match crate::modules::oauth_pending_state::load::<PendingOAuthFlowState>(
        OAUTH_PENDING_STATE_FILE,
    ) {
        Ok(Some(pending)) => match validate_pending_oauth_state(&pending) {
            Ok(_) => Some(pending),
            Err(error) => {
                crate::modules::logger::log_warn(&format!(
                    "Antigravity OAuth pending 会话无效，已清理: {}",
                    error
                ));
                let _ = clear_persisted_oauth_state_if_matches(pending.expected_state.as_str());
                None
            }
        },
        Ok(None) => None,
        Err(error) => {
            crate::modules::logger::log_warn(&format!(
                "读取 Antigravity OAuth pending 会话失败，已忽略: {}",
                error
            ));
            None
        }
    }
}

fn oauth_success_html() -> &'static str {
    "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n\r\n\
    <html>\
    <body style='font-family: sans-serif; text-align: center; padding: 50px; background: #0d1117; color: #fff;'>\
        <h1 style='color: #4ade80;'>授权成功</h1>\
        <p>您可以关闭此窗口返回应用。</p>\
        <script>setTimeout(function() { window.close(); }, 2000);</script>\
    </body>\
    </html>"
}

fn oauth_fail_html(message: &str) -> String {
    format!(
        "HTTP/1.1 400 Bad Request\r\nContent-Type: text/html; charset=utf-8\r\n\r\n\
    <html>\
    <body style='font-family: sans-serif; text-align: center; padding: 50px; background: #0d1117; color: #fff;'>\
        <h1 style='color: #f87171;'>授权失败</h1>\
        <p>{}</p>\
    </body>\
    </html>",
        message
    )
}

fn oauth_not_found_response() -> &'static str {
    "HTTP/1.1 404 Not Found\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nNot Found"
}

fn oauth_options_response() -> &'static str {
    "HTTP/1.1 200 OK\r\n\
    Access-Control-Allow-Origin: *\r\n\
    Access-Control-Allow-Methods: GET, OPTIONS\r\n\
    Access-Control-Allow-Headers: Content-Type\r\n\
    Content-Length: 0\r\n\r\n"
}

async fn write_response(stream: &mut tokio::net::TcpStream, response: &str) {
    let _ = stream.write_all(response.as_bytes()).await;
    let _ = stream.flush().await;
}

fn clear_oauth_flow_state_if_matches(expected_state: &str) -> Result<(), String> {
    let cancelled = {
        let mut lock = get_oauth_flow_state()
            .lock()
            .map_err(|_| "OAuth 状态锁被污染".to_string())?;
        match lock.as_ref() {
            Some(state) if state.pending.expected_state == expected_state => lock.take(),
            _ => None,
        }
    };
    if let Some(state) = cancelled {
        let _ = state.cancel_tx.send(true);
    }
    clear_persisted_oauth_state_if_matches(expected_state)
}

fn clear_oauth_flow_state() -> Result<(), String> {
    let cancelled = {
        let mut lock = get_oauth_flow_state()
            .lock()
            .map_err(|_| "OAuth 状态锁被污染".to_string())?;
        lock.take()
    };
    if let Some(state) = cancelled {
        let _ = state.cancel_tx.send(true);
    }
    persist_pending_oauth_state(None)
}

fn runtime_flow_matches_state(expected_state: &str) -> bool {
    get_oauth_flow_state()
        .lock()
        .ok()
        .and_then(|state| {
            state
                .as_ref()
                .map(|current| current.pending.expected_state == expected_state)
        })
        .unwrap_or(false)
}

fn callback_url_matches_redirect(callback_url: &Url, redirect_uri: &str) -> Result<(), String> {
    let (redirect, _) = parse_valid_loopback_redirect_uri(redirect_uri)?;
    let callback_host = callback_url
        .host_str()
        .ok_or_else(|| "回调链接缺少主机".to_string())?;
    let redirect_host = redirect
        .host_str()
        .ok_or_else(|| "OAuth redirect_uri 缺少主机".to_string())?;
    let same_host = callback_host.eq_ignore_ascii_case(redirect_host);

    if callback_url.scheme() != redirect.scheme()
        || !same_host
        || callback_url.port() != redirect.port()
        || callback_url.path() != OAUTH_CALLBACK_PATH
        || !callback_url.username().is_empty()
        || callback_url.password().is_some()
        || callback_url.fragment().is_some()
    {
        return Err("回调链接必须匹配当前本机 OAuth 回调地址".to_string());
    }
    Ok(())
}

fn extract_code_from_callback_url(
    callback_url: &Url,
    expected_state: &str,
) -> Result<String, String> {
    let mut code = None;
    let mut state = None;
    for (key, value) in callback_url.query_pairs() {
        match key.as_ref() {
            "code" if code.is_none() => code = Some(value.into_owned()),
            "state" if state.is_none() => state = Some(value.into_owned()),
            _ => {}
        }
    }

    let Some(state) = state.filter(|value| !value.trim().is_empty()) else {
        return Err("未能在回调中获取 OAuth state".to_string());
    };
    if state != expected_state {
        return Err("OAuth state 校验失败".to_string());
    }

    let Some(code) = code.filter(|value| !value.trim().is_empty()) else {
        return Err("未能在回调中获取 Authorization Code".to_string());
    };
    Ok(code)
}

fn callback_contains_oauth_error(callback_url: &Url) -> bool {
    callback_url
        .query_pairs()
        .any(|(key, value)| key == "error" && !value.trim().is_empty())
}

fn parse_manual_callback_url(raw_callback_url: &str, redirect_uri: &str) -> Result<Url, String> {
    let trimmed = raw_callback_url.trim();
    if trimmed.is_empty() {
        return Err("回调链接不能为空".to_string());
    }

    let (redirect, _) = parse_valid_loopback_redirect_uri(redirect_uri)?;
    if trimmed.starts_with("http://") || trimmed.starts_with("https://") {
        return Url::parse(trimmed).map_err(|_| "OAuth 回调 URL 解析失败".to_string());
    }

    let origin = redirect.origin().ascii_serialization();
    if trimmed.starts_with('/') {
        if trimmed.starts_with("//") {
            return Err("OAuth 回调 URL 解析失败".to_string());
        }
        return Url::parse(format!("{}{}", origin, trimmed).as_str())
            .map_err(|_| "OAuth 回调 URL 解析失败".to_string());
    }

    Url::parse(
        format!(
            "{}{}?{}",
            origin,
            OAUTH_CALLBACK_PATH,
            trimmed.trim_start_matches('?')
        )
        .as_str(),
    )
    .map_err(|_| "OAuth 回调 URL 解析失败".to_string())
}

async fn read_http_request(stream: &mut tokio::net::TcpStream) -> Result<String, String> {
    let mut buffer = Vec::with_capacity(4096);
    let mut chunk = [0u8; 2048];

    loop {
        let bytes_read = timeout(REQUEST_READ_TIMEOUT, stream.read(&mut chunk))
            .await
            .map_err(|_| "读取 OAuth 回调请求超时".to_string())?
            .map_err(|_| "读取 OAuth 回调请求失败".to_string())?;

        if bytes_read == 0 {
            break;
        }

        buffer.extend_from_slice(&chunk[..bytes_read]);
        if buffer.windows(4).any(|window| window == b"\r\n\r\n")
            || buffer.len() >= MAX_HTTP_REQUEST_BYTES
        {
            break;
        }
    }

    if buffer.is_empty() {
        return Err("OAuth 回调请求为空".to_string());
    }
    if buffer.len() >= MAX_HTTP_REQUEST_BYTES
        && !buffer.windows(4).any(|window| window == b"\r\n\r\n")
    {
        return Err("OAuth 回调请求过大".to_string());
    }

    Ok(String::from_utf8_lossy(&buffer).into_owned())
}

fn parse_request_target(request: &str) -> Result<(String, String), String> {
    let request_line = request
        .lines()
        .next()
        .ok_or_else(|| "OAuth 回调请求行为空".to_string())?;
    let mut parts = request_line.split_whitespace();
    let method = parts
        .next()
        .ok_or_else(|| "OAuth 回调请求缺少 method".to_string())?;
    let target = parts
        .next()
        .ok_or_else(|| "OAuth 回调请求缺少 target".to_string())?;
    if parts.next().is_none() {
        return Err("OAuth 回调请求版本无效".to_string());
    }
    Ok((method.to_string(), target.to_string()))
}

async fn process_callback_request(
    stream: &mut tokio::net::TcpStream,
    redirect_uri: &str,
    expected_state: &str,
) -> Option<Result<String, String>> {
    let request = match read_http_request(stream).await {
        Ok(request) => request,
        Err(_) => {
            write_response(stream, &oauth_fail_html("回调请求无效，请返回应用重试。")).await;
            return None;
        }
    };

    let (method, target) = match parse_request_target(&request) {
        Ok(parsed) => parsed,
        Err(_) => {
            write_response(
                stream,
                &oauth_fail_html("回调请求格式无效，请返回应用重试。"),
            )
            .await;
            return None;
        }
    };

    if method.eq_ignore_ascii_case("OPTIONS") {
        write_response(stream, oauth_options_response()).await;
        return None;
    }
    if !method.eq_ignore_ascii_case("GET") {
        write_response(stream, oauth_not_found_response()).await;
        return None;
    }
    if !target.starts_with('/') || target.starts_with("//") {
        write_response(stream, &oauth_fail_html("回调 URL 无效，请返回应用重试。")).await;
        return None;
    }

    let (redirect, _) = match parse_valid_loopback_redirect_uri(redirect_uri) {
        Ok(parsed) => parsed,
        Err(_) => {
            write_response(stream, &oauth_fail_html("OAuth 会话无效，请重新发起授权。")).await;
            return Some(Err("OAuth 会话无效，请重新发起授权".to_string()));
        }
    };
    let callback_url =
        match Url::parse(format!("{}{}", redirect.origin().ascii_serialization(), target).as_str())
        {
            Ok(url) => url,
            Err(_) => {
                write_response(
                    stream,
                    &oauth_fail_html("回调 URL 解析失败，请返回应用重试。"),
                )
                .await;
                return None;
            }
        };

    if callback_url.path() != OAUTH_CALLBACK_PATH {
        write_response(stream, oauth_not_found_response()).await;
        return None;
    }
    if callback_url_matches_redirect(&callback_url, redirect_uri).is_err() {
        write_response(stream, &oauth_fail_html("回调链接不是当前本机授权会话。")).await;
        return None;
    }

    let code = match extract_code_from_callback_url(&callback_url, expected_state) {
        Ok(code) => code,
        Err(error)
            if error == "OAuth state 校验失败" || error == "未能在回调中获取 OAuth state" =>
        {
            write_response(
                stream,
                &oauth_fail_html("授权状态校验失败，请重新发起授权。"),
            )
            .await;
            // A bad state must not consume an active local session: it could be a
            // stale browser tab or an unsolicited loopback request.
            return None;
        }
        Err(_) if callback_contains_oauth_error(&callback_url) => {
            write_response(
                stream,
                &oauth_fail_html("授权被取消或拒绝，请返回应用重新发起授权。"),
            )
            .await;
            return Some(Err("OAuth 授权被取消或拒绝，请重新发起授权".to_string()));
        }
        Err(_) => {
            write_response(
                stream,
                &oauth_fail_html("未能获取授权 code，请返回应用重试。"),
            )
            .await;
            return Some(Err("未能在回调中获取 Authorization Code".to_string()));
        }
    };

    write_response(stream, oauth_success_html()).await;
    Some(Ok(code))
}

fn make_runtime_flow(pending: PendingOAuthFlowState) -> OAuthFlowState {
    let auth_url = oauth_auth_url_for_pending(&pending);
    let (cancel_tx, _) = watch::channel(false);
    let (code_tx, code_rx) = oneshot::channel::<Result<String, String>>();
    OAuthFlowState {
        auth_url,
        pending,
        cancel_tx,
        code_tx: Arc::new(tokio::sync::Mutex::new(Some(code_tx))),
        code_rx: Some(code_rx),
    }
}

fn spawn_callback_listener(
    listener: TcpListener,
    app_handle: tauri::AppHandle,
    redirect_uri: String,
    expected_state: String,
    expires_at: i64,
    code_tx: Arc<tokio::sync::Mutex<Option<oneshot::Sender<Result<String, String>>>>>,
    mut cancel_rx: watch::Receiver<bool>,
) {
    use tauri::Emitter;

    tokio::spawn(async move {
        loop {
            let remaining_seconds = expires_at.saturating_sub(now_timestamp());
            if remaining_seconds <= 0 {
                if let Some(sender) = code_tx.lock().await.take() {
                    let _ = sender.send(Err("等待 OAuth 回调超时，请重试".to_string()));
                }
                let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
                break;
            }

            let accept_result = tokio::select! {
                result = listener.accept() => Some(result),
                _ = cancel_rx.changed() => None,
                _ = tokio::time::sleep(Duration::from_secs(remaining_seconds as u64)) => {
                    if let Some(sender) = code_tx.lock().await.take() {
                        let _ = sender.send(Err("等待 OAuth 回调超时，请重试".to_string()));
                    }
                    let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
                    None
                },
            };

            let Some(accept_result) = accept_result else {
                break;
            };
            let Ok((mut stream, peer_addr)) = accept_result else {
                continue;
            };
            if !peer_addr.ip().is_loopback() {
                continue;
            }

            let result = process_callback_request(
                &mut stream,
                redirect_uri.as_str(),
                expected_state.as_str(),
            )
            .await;
            if let Some(result) = result {
                if let Some(sender) = code_tx.lock().await.take() {
                    let _ = app_handle.emit("oauth-callback-received", ());
                    let _ = sender.send(result);
                }
                break;
            }
        }
    });
}

async fn install_runtime_flow(
    listener: TcpListener,
    pending: PendingOAuthFlowState,
    app_handle: &tauri::AppHandle,
) -> Result<Option<String>, String> {
    let runtime = make_runtime_flow(pending);
    let auth_url = runtime.auth_url.clone();
    let redirect_uri = runtime.pending.redirect_uri.clone();
    let expected_state = runtime.pending.expected_state.clone();
    let expires_at = runtime.pending.expires_at;
    let code_tx = runtime.code_tx.clone();
    let cancel_rx = runtime.cancel_tx.subscribe();

    let installed = {
        let mut lock = get_oauth_flow_state()
            .lock()
            .map_err(|_| "OAuth 状态锁被污染".to_string())?;
        if lock.is_some() {
            false
        } else {
            *lock = Some(runtime);
            true
        }
    };
    if !installed {
        return Ok(None);
    }

    spawn_callback_listener(
        listener,
        app_handle.clone(),
        redirect_uri,
        expected_state,
        expires_at,
        code_tx,
        cancel_rx,
    );
    Ok(Some(auth_url))
}

async fn hydrate_pending_oauth_flow_if_missing(
    app_handle: &tauri::AppHandle,
) -> Result<(), String> {
    let is_missing = get_oauth_flow_state()
        .lock()
        .map_err(|_| "OAuth 状态锁被污染".to_string())?
        .is_none();
    if !is_missing {
        return Ok(());
    }

    let Some(pending) = load_pending_oauth_state_from_disk() else {
        return Ok(());
    };
    let port = match validate_pending_oauth_state(&pending) {
        Ok(port) => port,
        Err(error) => {
            let _ = clear_persisted_oauth_state_if_matches(pending.expected_state.as_str());
            return Err(error);
        }
    };
    let listener = match TcpListener::bind(("127.0.0.1", port)).await {
        Ok(listener) => listener,
        Err(error) => {
            // The redirect URI is pinned to this port. Never pretend recovery is
            // possible when another process owns it; discard the stale session.
            if runtime_flow_matches_state(pending.expected_state.as_str()) {
                return Ok(());
            }
            let _ = clear_persisted_oauth_state_if_matches(pending.expected_state.as_str());
            return Err(format!("无法恢复 OAuth 本机回调监听: {}", error));
        }
    };

    let _ = install_runtime_flow(listener, pending, app_handle).await?;
    Ok(())
}

fn current_auth_url_if_valid() -> Result<Option<String>, String> {
    let snapshot = {
        let lock = get_oauth_flow_state()
            .lock()
            .map_err(|_| "OAuth 状态锁被污染".to_string())?;
        lock.as_ref().map(|state| {
            (
                state.auth_url.clone(),
                state.pending.expected_state.clone(),
                state.pending.expires_at,
            )
        })
    };
    match snapshot {
        Some((_, expected_state, expires_at)) if expires_at <= now_timestamp() => {
            let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
            Ok(None)
        }
        Some((auth_url, _, _)) => Ok(Some(auth_url)),
        None => Ok(None),
    }
}

async fn ensure_oauth_flow_prepared(app_handle: &tauri::AppHandle) -> Result<String, String> {
    use tauri::Emitter;

    if let Some(auth_url) = current_auth_url_if_valid()? {
        return Ok(auth_url);
    }
    hydrate_pending_oauth_flow_if_missing(app_handle).await?;
    if let Some(auth_url) = current_auth_url_if_valid()? {
        return Ok(auth_url);
    }

    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .map_err(|error| format!("无法绑定本地 OAuth 回调端口: {}", error))?;
    let port = listener
        .local_addr()
        .map_err(|error| format!("无法读取本地 OAuth 回调端口: {}", error))?
        .port();
    let created_at = now_timestamp();
    let pending = PendingOAuthFlowState {
        // Keep localhost for compatibility with the existing installed OAuth
        // client. The listener itself remains bound to 127.0.0.1.
        redirect_uri: format!("http://localhost:{}{}", port, OAUTH_CALLBACK_PATH),
        expected_state: generate_base64url_token(32),
        code_verifier: generate_base64url_token(32),
        created_at,
        expires_at: created_at + OAUTH_FLOW_TIMEOUT_SECONDS,
    };
    validate_pending_oauth_state(&pending)?;
    persist_pending_oauth_state(Some(&pending))?;

    match install_runtime_flow(listener, pending, app_handle).await? {
        Some(auth_url) => {
            let _ = app_handle.emit("oauth-url-generated", &auth_url);
            Ok(auth_url)
        }
        None => current_auth_url_if_valid()?
            .ok_or_else(|| "OAuth 登录会话已变更，请重新发起授权".to_string()),
    }
}

/// 预生成 OAuth URL。
pub async fn prepare_oauth_url(app_handle: tauri::AppHandle) -> Result<String, String> {
    ensure_oauth_flow_prepared(&app_handle).await
}

/// 取消当前的 OAuth 流程并删除可恢复的 pending 状态。
pub fn cancel_oauth_flow() -> Result<(), String> {
    clear_oauth_flow_state()
}

pub async fn submit_oauth_callback_url(
    app_handle: tauri::AppHandle,
    callback_url: &str,
) -> Result<(), String> {
    use tauri::Emitter;

    hydrate_pending_oauth_flow_if_missing(&app_handle).await?;
    let (redirect_uri, expected_state, expires_at, code_tx, cancel_tx) = {
        let lock = get_oauth_flow_state()
            .lock()
            .map_err(|_| "OAuth 状态锁被污染".to_string())?;
        let state = lock
            .as_ref()
            .ok_or_else(|| "OAuth 状态不存在，请先发起授权".to_string())?;
        (
            state.pending.redirect_uri.clone(),
            state.pending.expected_state.clone(),
            state.pending.expires_at,
            state.code_tx.clone(),
            state.cancel_tx.clone(),
        )
    };
    if expires_at <= now_timestamp() {
        let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
        return Err("OAuth 授权已超时，请重新发起授权".to_string());
    }

    let parsed = parse_manual_callback_url(callback_url, redirect_uri.as_str())?;
    callback_url_matches_redirect(&parsed, redirect_uri.as_str())?;
    let code = extract_code_from_callback_url(&parsed, expected_state.as_str())?;

    let mut sender = code_tx.lock().await;
    let sender = sender
        .take()
        .ok_or_else(|| "OAuth 回调已处理，请勿重复提交".to_string())?;
    sender
        .send(Ok(code))
        .map_err(|_| "OAuth 回调发送失败，请重新发起授权".to_string())?;
    let _ = cancel_tx.send(true);
    let _ = app_handle.emit("oauth-callback-received", ());
    Ok(())
}

fn take_callback_receiver() -> Result<
    (
        oneshot::Receiver<Result<String, String>>,
        String,
        String,
        String,
    ),
    String,
> {
    let snapshot = {
        let mut lock = get_oauth_flow_state()
            .lock()
            .map_err(|_| "OAuth 状态锁被污染".to_string())?;
        let state = lock
            .as_mut()
            .ok_or_else(|| "OAuth 状态不存在".to_string())?;
        if state.pending.expires_at <= now_timestamp() {
            Some(Err(state.pending.expected_state.clone()))
        } else {
            let receiver = state
                .code_rx
                .take()
                .ok_or_else(|| "OAuth 授权已在进行中".to_string())?;
            Some(Ok((
                receiver,
                state.pending.redirect_uri.clone(),
                state.pending.code_verifier.clone(),
                state.pending.expected_state.clone(),
            )))
        }
    };

    match snapshot.expect("OAuth callback receiver snapshot is always set") {
        Ok(value) => Ok(value),
        Err(expected_state) => {
            let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
            Err("OAuth 授权已超时，请重新发起授权".to_string())
        }
    }
}

async fn wait_for_oauth_code() -> Result<(String, String, String), String> {
    let (code_rx, redirect_uri, code_verifier, expected_state) = take_callback_receiver()?;
    let callback_result = timeout(OAUTH_FLOW_WAIT_TIMEOUT, code_rx).await;
    match callback_result {
        Ok(Ok(Ok(code))) => Ok((code, redirect_uri, code_verifier)),
        Ok(Ok(Err(error))) => {
            let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
            Err(error)
        }
        Ok(Err(_)) => {
            let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
            Err("等待 OAuth 回调失败".to_string())
        }
        Err(_) => {
            let _ = clear_oauth_flow_state_if_matches(expected_state.as_str());
            Err("等待 OAuth 回调超时，请重试".to_string())
        }
    }
}

/// 启动 OAuth 流程并等待回调。
pub async fn start_oauth_flow(
    app_handle: tauri::AppHandle,
) -> Result<oauth::TokenResponse, String> {
    let auth_url = ensure_oauth_flow_prepared(&app_handle).await?;

    use tauri_plugin_opener::OpenerExt;
    if let Err(error) = app_handle.opener().open_url(&auth_url, None::<String>) {
        if let Err(cleanup_error) = cancel_oauth_flow() {
            return Err(format!(
                "无法打开浏览器: {}; OAuth 登录会话清理失败: {}",
                error, cleanup_error
            ));
        }
        return Err(format!("无法打开浏览器: {}", error));
    }

    let (code, redirect_uri, code_verifier) = wait_for_oauth_code().await?;
    clear_oauth_flow_state()
        .map_err(|error| format!("OAuth 授权会话清理失败，请重新发起授权: {}", error))?;
    oauth::exchange_code(&code, &redirect_uri, &code_verifier).await
}

/// 完成已在浏览器中打开的 OAuth 流程。
pub async fn complete_oauth_flow(
    app_handle: tauri::AppHandle,
) -> Result<oauth::TokenResponse, String> {
    let _ = ensure_oauth_flow_prepared(&app_handle).await?;
    let (code, redirect_uri, code_verifier) = wait_for_oauth_code().await?;
    clear_oauth_flow_state()
        .map_err(|error| format!("OAuth 授权会话清理失败，请重新发起授权: {}", error))?;
    oauth::exchange_code(&code, &redirect_uri, &code_verifier).await
}

/// 应用重启后仅恢复仍有效的本机 loopback 回调监听。
pub async fn restore_pending_oauth_listener(app_handle: tauri::AppHandle) {
    if let Err(error) = hydrate_pending_oauth_flow_if_missing(&app_handle).await {
        crate::modules::logger::log_warn(&format!(
            "Antigravity OAuth pending 回调恢复失败，已忽略: {}",
            error
        ));
    }
}

#[cfg(test)]
mod tests {
    use super::{
        callback_url_matches_redirect, extract_code_from_callback_url, is_valid_base64url_token,
        oauth_auth_url_for_pending, parse_manual_callback_url, parse_valid_loopback_redirect_uri,
        pkce_code_challenge, process_callback_request, validate_pending_oauth_state_at,
        PendingOAuthFlowState, OAUTH_FLOW_TIMEOUT_SECONDS,
    };
    use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
    use sha2::{Digest, Sha256};
    use tauri::Url;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::{TcpListener, TcpStream};

    fn valid_pending(now: i64) -> PendingOAuthFlowState {
        PendingOAuthFlowState {
            redirect_uri: "http://localhost:49152/oauth-callback".to_string(),
            expected_state: URL_SAFE_NO_PAD.encode([7_u8; 32]),
            code_verifier: URL_SAFE_NO_PAD.encode([9_u8; 32]),
            created_at: now,
            expires_at: now + OAUTH_FLOW_TIMEOUT_SECONDS,
        }
    }

    #[test]
    fn pending_state_requires_short_lived_loopback_pkce_session() {
        let now = 1_700_000_000;
        let pending = valid_pending(now);
        assert_eq!(
            validate_pending_oauth_state_at(&pending, now + 1),
            Ok(49152)
        );

        let mut expired = pending.clone();
        expired.expires_at = now;
        assert!(validate_pending_oauth_state_at(&expired, now + 1).is_err());

        let mut remote_redirect = pending;
        remote_redirect.redirect_uri = "https://example.invalid/oauth-callback".to_string();
        assert!(validate_pending_oauth_state_at(&remote_redirect, now + 1).is_err());
    }

    #[test]
    fn pending_state_rejects_invalid_state_or_verifier() {
        let now = 1_700_000_000;
        let mut pending = valid_pending(now);
        pending.expected_state = "not-a-valid-random-token".to_string();
        assert!(validate_pending_oauth_state_at(&pending, now + 1).is_err());

        pending.expected_state = URL_SAFE_NO_PAD.encode([7_u8; 32]);
        pending.code_verifier = "short".to_string();
        assert!(validate_pending_oauth_state_at(&pending, now + 1).is_err());
        assert!(is_valid_base64url_token(
            &URL_SAFE_NO_PAD.encode([7_u8; 32]),
            43,
            128
        ));
    }

    #[test]
    fn persisted_state_contains_only_restart_recovery_material() {
        let pending = valid_pending(1_700_000_000);
        let serialized = serde_json::to_value(&pending).expect("pending state serializes");
        let keys = serialized
            .as_object()
            .expect("pending state is an object")
            .keys()
            .map(String::as_str)
            .collect::<std::collections::BTreeSet<_>>();
        assert_eq!(
            keys,
            std::collections::BTreeSet::from([
                "code_verifier",
                "created_at",
                "expected_state",
                "expires_at",
                "redirect_uri",
            ])
        );
        assert!(!serialized.to_string().contains("access_token"));
        assert!(!serialized.to_string().contains("refresh_token"));
    }

    #[test]
    fn authorization_url_uses_the_same_s256_challenge_as_persisted_verifier() {
        let pending = valid_pending(1_700_000_000);
        let auth_url = oauth_auth_url_for_pending(&pending);
        let parsed = Url::parse(&auth_url).expect("authorization URL parses");
        let query = parsed
            .query_pairs()
            .collect::<std::collections::HashMap<_, _>>();

        let mut hasher = Sha256::new();
        hasher.update(pending.code_verifier.as_bytes());
        let expected_challenge = URL_SAFE_NO_PAD.encode(hasher.finalize());
        assert_eq!(
            pkce_code_challenge(&pending.code_verifier),
            expected_challenge
        );
        assert_eq!(
            query.get("code_challenge").map(|value| value.as_ref()),
            Some(expected_challenge.as_str())
        );
        assert_eq!(
            query
                .get("code_challenge_method")
                .map(|value| value.as_ref()),
            Some("S256")
        );
    }

    #[test]
    fn manual_callback_must_match_the_exact_current_loopback_origin() {
        let redirect = "http://localhost:49152/oauth-callback";
        let valid = parse_manual_callback_url("?code=abc&state=state", redirect)
            .expect("relative callback parses");
        assert!(callback_url_matches_redirect(&valid, redirect).is_ok());

        let wrong_host = Url::parse("http://127.0.0.1:49152/oauth-callback?code=abc&state=state")
            .expect("callback parses");
        assert!(callback_url_matches_redirect(&wrong_host, redirect).is_err());

        let wrong_port = Url::parse("http://localhost:49153/oauth-callback?code=abc&state=state")
            .expect("callback parses");
        assert!(callback_url_matches_redirect(&wrong_port, redirect).is_err());

        assert!(parse_manual_callback_url("//example.invalid/oauth-callback", redirect).is_err());
        assert!(
            parse_valid_loopback_redirect_uri("https://localhost:49152/oauth-callback").is_err()
        );
    }

    #[test]
    fn callback_code_is_accepted_only_after_state_validation() {
        let expected_state = URL_SAFE_NO_PAD.encode([3_u8; 32]);
        let valid = Url::parse(
            format!(
                "http://localhost:49152/oauth-callback?code=authorization-code&state={}",
                expected_state
            )
            .as_str(),
        )
        .expect("valid callback");
        assert_eq!(
            extract_code_from_callback_url(&valid, expected_state.as_str()),
            Ok("authorization-code".to_string())
        );

        let invalid = Url::parse(
            "http://localhost:49152/oauth-callback?code=authorization-code&state=wrong-state",
        )
        .expect("invalid-state callback parses");
        assert!(extract_code_from_callback_url(&invalid, expected_state.as_str()).is_err());
    }

    #[tokio::test]
    async fn loopback_callback_with_bad_state_does_not_consume_the_active_flow() {
        let listener = TcpListener::bind("127.0.0.1:0")
            .await
            .expect("bind loopback listener");
        let port = listener.local_addr().expect("listener address").port();
        let expected_state = URL_SAFE_NO_PAD.encode([4_u8; 32]);
        let redirect_uri = format!("http://localhost:{}/oauth-callback", port);
        let server_redirect_uri = redirect_uri.clone();
        let server_expected_state = expected_state.clone();
        let server = tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.expect("accept callback");
            process_callback_request(
                &mut stream,
                server_redirect_uri.as_str(),
                server_expected_state.as_str(),
            )
            .await
        });

        let mut client = TcpStream::connect(("127.0.0.1", port))
            .await
            .expect("connect callback listener");
        let request = format!(
            "GET /oauth-callback?code=test-code&state=wrong HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
        );
        client
            .write_all(request.as_bytes())
            .await
            .expect("write callback request");
        let mut response = Vec::new();
        client
            .read_to_end(&mut response)
            .await
            .expect("read callback response");

        assert!(String::from_utf8_lossy(&response).starts_with("HTTP/1.1 400"));
        assert!(server.await.expect("join callback task").is_none());
    }

    #[tokio::test]
    async fn valid_loopback_callback_delivers_only_the_authorization_code() {
        let listener = TcpListener::bind("127.0.0.1:0")
            .await
            .expect("bind loopback listener");
        let port = listener.local_addr().expect("listener address").port();
        let expected_state = URL_SAFE_NO_PAD.encode([5_u8; 32]);
        let redirect_uri = format!("http://localhost:{}/oauth-callback", port);
        let server_redirect_uri = redirect_uri.clone();
        let server_expected_state = expected_state.clone();
        let server = tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.expect("accept callback");
            process_callback_request(
                &mut stream,
                server_redirect_uri.as_str(),
                server_expected_state.as_str(),
            )
            .await
        });

        let mut client = TcpStream::connect(("127.0.0.1", port))
            .await
            .expect("connect callback listener");
        let request = format!(
            "GET /oauth-callback?code=test-code&state={} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
            expected_state
        );
        client
            .write_all(request.as_bytes())
            .await
            .expect("write callback request");
        let mut response = Vec::new();
        client
            .read_to_end(&mut response)
            .await
            .expect("read callback response");

        assert!(String::from_utf8_lossy(&response).starts_with("HTTP/1.1 200"));
        assert_eq!(
            server.await.expect("join callback task"),
            Some(Ok("test-code".to_string()))
        );
    }
}
