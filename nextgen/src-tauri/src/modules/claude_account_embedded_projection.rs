// Renderer-safe Claude Code account projection for the embedded WF workspace.
// The Tauri account vault remains the only owner of credentials and paths.

const EMBEDDED_CLAUDE_CODE_MAX_CANDIDATES: usize = 24;
const EMBEDDED_CLAUDE_CODE_MAX_MODELS_PER_CANDIDATE: usize = 24;
const EMBEDDED_CLAUDE_CODE_MAX_ACCOUNT_ID_BYTES: usize = 128;
const EMBEDDED_CLAUDE_CODE_MAX_LABEL_CHARS: usize = 160;
const EMBEDDED_CLAUDE_CODE_MAX_MODEL_ID_BYTES: usize = 256;
const EMBEDDED_CLAUDE_CODE_APPLIED_MESSAGE: &str = "已由 XIASS Tools 应用所选 Claude Code 账户。";
const EMBEDDED_CLAUDE_CODE_CURRENT_ACCOUNT_MARKER_WARNING: &str =
    "已应用所选 Claude Code 账户；当前账户标记未保存，不影响 Claude Code 使用。";

const EMBEDDED_CLAUDE_CODE_DEFAULT_MODELS: &[&str] = &[
    "claude-opus-4-8",
    "claude-fable-5",
    "claude-opus-4-7",
    "claude-opus-4-6",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
];

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct EmbeddedClaudeCodeCandidateModel {
    pub id: String,
    pub display_name: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct EmbeddedClaudeCodeAccountCandidate {
    pub id: String,
    pub label: String,
    pub credential_mode: String,
    pub models: Vec<EmbeddedClaudeCodeCandidateModel>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct EmbeddedClaudeCodeCandidatesResult {
    pub ok: bool,
    pub message: String,
    pub candidates: Vec<EmbeddedClaudeCodeAccountCandidate>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct EmbeddedClaudeCodeApplyResult {
    pub ok: bool,
    pub message: String,
}

fn embedded_claude_code_safe_account_id(value: &str) -> Option<String> {
    let value = value.trim();
    if value.is_empty()
        || value.len() > EMBEDDED_CLAUDE_CODE_MAX_ACCOUNT_ID_BYTES
        || value.chars().any(char::is_control)
        || !value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'-'))
    {
        return None;
    }
    Some(value.to_string())
}

fn embedded_claude_code_safe_model_id(value: &str) -> Option<String> {
    let value = value.trim();
    if value.is_empty()
        || value.len() > EMBEDDED_CLAUDE_CODE_MAX_MODEL_ID_BYTES
        || value.chars().any(char::is_control)
        || !value.bytes().all(|byte| {
            byte.is_ascii_alphanumeric()
                || matches!(
                    byte,
                    b'.' | b'_' | b':' | b'/' | b'@' | b'+' | b'-' | b'[' | b']'
                )
        })
    {
        return None;
    }
    Some(value.to_string())
}

fn embedded_claude_code_safe_label(value: &str) -> Option<String> {
    let value = value.trim();
    if value.is_empty()
        || value.chars().count() > EMBEDDED_CLAUDE_CODE_MAX_LABEL_CHARS
        || value.chars().any(char::is_control)
    {
        return None;
    }
    Some(value.to_string())
}

fn embedded_claude_code_credential_mode(account: &ClaudeAccount) -> Option<&'static str> {
    match account.auth_mode {
        ClaudeAuthMode::OAuth if embedded_claude_code_has_oauth_snapshot(account) => Some("OAuth"),
        ClaudeAuthMode::SetupToken if embedded_claude_code_has_oauth_snapshot(account) => {
            Some("Setup Token")
        }
        ClaudeAuthMode::ApiKey
            if account
                .api_key
                .as_deref()
                .is_some_and(|value| !value.trim().is_empty()) =>
        {
            Some("API Key")
        }
        ClaudeAuthMode::DesktopOAuth | ClaudeAuthMode::DesktopGateway => None,
        _ => None,
    }
}

fn embedded_claude_code_has_oauth_snapshot(account: &ClaudeAccount) -> bool {
    account
        .claude_credentials_raw
        .as_ref()
        .is_some_and(|snapshot| snapshot.get("claudeAiOauth").is_some())
        && account
            .claude_config_raw
            .as_ref()
            .is_some_and(|snapshot| snapshot.get("oauthAccount").is_some())
}

fn embedded_claude_code_model_ids(
    account: &ClaudeAccount,
) -> Vec<EmbeddedClaudeCodeCandidateModel> {
    let source = account
        .api_model_catalog
        .as_deref()
        .filter(|models| !models.is_empty())
        .map(|models| models.iter().map(String::as_str).collect::<Vec<_>>())
        .unwrap_or_else(|| EMBEDDED_CLAUDE_CODE_DEFAULT_MODELS.to_vec());
    let mut seen = BTreeSet::new();
    let mut models = Vec::new();
    for raw in source {
        let Some(id) = embedded_claude_code_safe_model_id(raw) else {
            continue;
        };
        let key = id.to_ascii_lowercase();
        if !seen.insert(key) {
            continue;
        }
        models.push(EmbeddedClaudeCodeCandidateModel {
            display_name: id.clone(),
            id,
        });
        if models.len() == EMBEDDED_CLAUDE_CODE_MAX_MODELS_PER_CANDIDATE {
            break;
        }
    }
    models
}

fn embedded_claude_code_candidate(
    account: &ClaudeAccount,
) -> Option<EmbeddedClaudeCodeAccountCandidate> {
    let id = embedded_claude_code_safe_account_id(&account.id)?;
    let credential_mode = embedded_claude_code_credential_mode(account)?;
    let label = embedded_claude_code_safe_label(&account.email)
        .unwrap_or_else(|| "Claude Code 账号".to_string());
    let models = embedded_claude_code_model_ids(account);
    if models.is_empty() {
        return None;
    }
    Some(EmbeddedClaudeCodeAccountCandidate {
        id,
        label,
        credential_mode: credential_mode.to_string(),
        models,
    })
}

fn embedded_claude_code_candidates_from_accounts(
    accounts: &[ClaudeAccount],
) -> Vec<EmbeddedClaudeCodeAccountCandidate> {
    let mut candidates = accounts
        .iter()
        .filter_map(embedded_claude_code_candidate)
        .collect::<Vec<_>>();
    candidates.sort_by(|left, right| {
        left.label
            .to_ascii_lowercase()
            .cmp(&right.label.to_ascii_lowercase())
            .then_with(|| left.id.cmp(&right.id))
    });
    candidates.truncate(EMBEDDED_CLAUDE_CODE_MAX_CANDIDATES);
    candidates
}

pub(crate) fn embedded_claude_code_account_candidates(
) -> Result<EmbeddedClaudeCodeCandidatesResult, String> {
    let accounts =
        list_accounts_checked().map_err(|_| "无法读取 XIASS Tools Claude 账号库。".to_string())?;
    let candidates = embedded_claude_code_candidates_from_accounts(&accounts);
    let message = if candidates.is_empty() {
        "没有可直接用于 Claude Code 的 XIASS Tools 账户。".to_string()
    } else {
        "已读取 XIASS Tools 中可用于 Claude Code 的账户。".to_string()
    };
    Ok(EmbeddedClaudeCodeCandidatesResult {
        ok: true,
        message,
        candidates,
    })
}

// The embedded helper writes its selected Claude Code model before asking the
// Tauri host to inject credentials. Once credential injection has committed,
// failure to persist the optional "current account" UI marker must not turn
// the operation into an error: the helper would otherwise restore the model
// while the newly injected credential remains in Claude Code.
//
// Keep the credential/config commit as the only success boundary. The current
// account marker is derived display state and is recorded best-effort after
// that boundary. Its failure is visible to the caller, but it is never allowed
// to trigger a misleading rollback of the already-committed model selection.
fn finalize_embedded_claude_code_account_application<Inject, MarkCurrent>(
    inject: Inject,
    mark_current: MarkCurrent,
) -> Result<EmbeddedClaudeCodeApplyResult, String>
where
    Inject: FnOnce() -> Result<(), String>,
    MarkCurrent: FnOnce() -> Result<(), String>,
{
    inject().map_err(|_| "无法将所选 XIASS Tools 账户应用到 Claude Code。".to_string())?;

    let message = match mark_current() {
        Ok(()) => EMBEDDED_CLAUDE_CODE_APPLIED_MESSAGE.to_string(),
        Err(_) => {
            logger::log_warn(
                "[Claude Code] 账户凭据已应用，但当前账户展示标记未保存；不会回滚已提交的 Claude Code 配置。",
            );
            EMBEDDED_CLAUDE_CODE_CURRENT_ACCOUNT_MARKER_WARNING.to_string()
        }
    };

    Ok(EmbeddedClaudeCodeApplyResult { ok: true, message })
}

pub(crate) fn apply_embedded_claude_code_account_candidate(
    account_id: &str,
    model: &str,
) -> Result<EmbeddedClaudeCodeApplyResult, String> {
    let account_id = embedded_claude_code_safe_account_id(account_id)
        .ok_or_else(|| "所选 Claude Code 账户无效。".to_string())?;
    let model = embedded_claude_code_safe_model_id(model)
        .ok_or_else(|| "所选 Claude Code 模型无效。".to_string())?;
    let account =
        load_account(&account_id).ok_or_else(|| "所选 Claude Code 账户不可用。".to_string())?;
    let candidate = embedded_claude_code_candidate(&account)
        .ok_or_else(|| "所选 Claude Code 账户不能安全用于此配置。".to_string())?;
    if !candidate.models.iter().any(|item| item.id == model) {
        return Err("所选模型不属于该 Claude Code 账户。".to_string());
    }

    finalize_embedded_claude_code_account_application(
        || inject_to_claude_config(&account_id, None),
        || {
            crate::modules::provider_current_state::set_current_account_id(
                "claude_code_account",
                Some(&account_id),
            )
        },
    )
}
