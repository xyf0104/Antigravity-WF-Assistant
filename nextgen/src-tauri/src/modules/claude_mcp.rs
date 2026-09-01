//! Managed Claude Code MCP operations.
//!
//! This module intentionally delegates MCP ownership to the installed Claude
//! Code CLI. It never reads or rewrites Claude's own configuration or session
//! files. The renderer receives only a small status projection; command output,
//! executable paths and remote endpoints stay on the native side.

use serde::Serialize;
use std::collections::BTreeSet;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Duration;
use url::Url;

const MANAGED_SERVER_NAME: &str = "xiass-tools";
const CLI_TIMEOUT: Duration = Duration::from_secs(5);
const MAX_REMOTE_URL_BYTES: usize = 2_048;

const CLI_UNAVAILABLE_MESSAGE: &str = "未检测到 Claude Code CLI。";
const STATUS_UNKNOWN_MESSAGE: &str = "无法检查受管 Claude Code MCP。";
const ADD_FAILED_MESSAGE: &str = "无法配置受管 Claude Code MCP。";
const REMOVE_FAILED_MESSAGE: &str = "无法移除受管 Claude Code MCP。";
const INVALID_URL_MESSAGE: &str = "MCP 地址无效，仅支持不含凭据或查询参数的 HTTP(S) 地址。";

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeManagedMcpStatus {
    pub cli_available: bool,
    pub managed_server_configured: bool,
    pub state: String,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ClaudeManagedMcpMutationResult {
    pub ok: bool,
    pub state: String,
    pub message: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ClaudeMcpCommandOutput {
    success: bool,
    stdout: Vec<u8>,
    stderr: Vec<u8>,
}

impl ClaudeMcpCommandOutput {
    fn from_process_output(output: std::process::Output) -> Self {
        Self {
            success: output.status.success(),
            stdout: output.stdout,
            stderr: output.stderr,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ClaudeMcpError {
    CliUnavailable,
    CommandFailed,
    InvalidRemoteUrl,
}

impl ClaudeMcpError {
    fn public_message(self, operation: ClaudeMcpOperation) -> &'static str {
        match self {
            Self::CliUnavailable => CLI_UNAVAILABLE_MESSAGE,
            Self::InvalidRemoteUrl => INVALID_URL_MESSAGE,
            Self::CommandFailed => match operation {
                ClaudeMcpOperation::Add => ADD_FAILED_MESSAGE,
                ClaudeMcpOperation::Remove => REMOVE_FAILED_MESSAGE,
                ClaudeMcpOperation::List => STATUS_UNKNOWN_MESSAGE,
            },
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ClaudeMcpOperation {
    List,
    Add,
    Remove,
}

impl ClaudeMcpOperation {
    fn args(self, remote_url: Option<&str>) -> Result<Vec<String>, ClaudeMcpError> {
        match self {
            Self::List => Ok(vec!["mcp".to_string(), "list".to_string()]),
            Self::Add => Ok(vec![
                "mcp".to_string(),
                "add".to_string(),
                "--transport".to_string(),
                "http".to_string(),
                "--scope".to_string(),
                "user".to_string(),
                MANAGED_SERVER_NAME.to_string(),
                remote_url
                    .map(str::to_string)
                    .ok_or(ClaudeMcpError::InvalidRemoteUrl)?,
            ]),
            // Claude Code documents removal by entry name. Keep the name fixed
            // so this adapter cannot remove a renderer-supplied entry.
            Self::Remove => Ok(vec![
                "mcp".to_string(),
                "remove".to_string(),
                MANAGED_SERVER_NAME.to_string(),
            ]),
        }
    }
}

fn managed_status(
    cli_available: bool,
    managed_server_configured: bool,
    state: &str,
    message: &str,
) -> ClaudeManagedMcpStatus {
    ClaudeManagedMcpStatus {
        cli_available,
        managed_server_configured,
        state: state.to_string(),
        message: message.to_string(),
    }
}

fn system_cli_candidates() -> Vec<PathBuf> {
    let mut candidates = Vec::new();
    if let Some(home) = dirs::home_dir() {
        #[cfg(target_os = "windows")]
        {
            candidates.push(home.join(".local").join("bin").join("claude.exe"));
            candidates.push(
                home.join("AppData")
                    .join("Roaming")
                    .join("npm")
                    .join("claude.cmd"),
            );
        }
        #[cfg(not(target_os = "windows"))]
        {
            candidates.push(home.join(".local").join("bin").join("claude"));
            candidates.push(home.join(".npm-global").join("bin").join("claude"));
        }
    }
    #[cfg(target_os = "macos")]
    {
        candidates.push(PathBuf::from("/opt/homebrew/bin/claude"));
        candidates.push(PathBuf::from("/usr/local/bin/claude"));
    }
    #[cfg(target_os = "linux")]
    {
        candidates.push(PathBuf::from("/usr/local/bin/claude"));
        candidates.push(PathBuf::from("/usr/bin/claude"));
    }

    // The final candidate lets the OS resolve an installed CLI from PATH.
    candidates.push(PathBuf::from("claude"));

    let mut seen = BTreeSet::new();
    candidates
        .into_iter()
        .filter(|candidate| seen.insert(candidate.to_string_lossy().into_owned()))
        .collect()
}

fn run_system_claude_cli(
    cli_path: &Path,
    args: &[String],
) -> Result<ClaudeMcpCommandOutput, ClaudeMcpError> {
    let mut command = Command::new(cli_path);
    command.args(args);
    #[cfg(target_os = "windows")]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x0800_0000);
    }

    crate::modules::process_timeout::output_with_timeout(&mut command, CLI_TIMEOUT)
        .map(ClaudeMcpCommandOutput::from_process_output)
        .map_err(|_| ClaudeMcpError::CommandFailed)
}

fn discover_claude_cli_with<Run>(run: &mut Run) -> Result<PathBuf, ClaudeMcpError>
where
    Run: FnMut(&Path, &[String]) -> Result<ClaudeMcpCommandOutput, ClaudeMcpError>,
{
    let probe_args = vec!["--version".to_string()];
    for candidate in system_cli_candidates() {
        let is_path_lookup = candidate == Path::new("claude");
        if !is_path_lookup && !candidate.is_file() {
            continue;
        }
        if let Ok(output) = run(&candidate, &probe_args) {
            if output.success {
                return Ok(candidate);
            }
        }
    }
    Err(ClaudeMcpError::CliUnavailable)
}

fn discover_claude_cli() -> Result<PathBuf, ClaudeMcpError> {
    discover_claude_cli_with(&mut run_system_claude_cli)
}

fn validate_remote_mcp_url(raw_url: &str) -> Result<String, ClaudeMcpError> {
    let value = raw_url.trim();
    if value.is_empty() || value.len() > MAX_REMOTE_URL_BYTES {
        return Err(ClaudeMcpError::InvalidRemoteUrl);
    }
    let parsed = Url::parse(value).map_err(|_| ClaudeMcpError::InvalidRemoteUrl)?;
    if !matches!(parsed.scheme(), "http" | "https")
        || parsed.host_str().is_none()
        || !parsed.username().is_empty()
        || parsed.password().is_some()
        || parsed.query().is_some()
        || parsed.fragment().is_some()
    {
        return Err(ClaudeMcpError::InvalidRemoteUrl);
    }
    Ok(parsed.to_string())
}

fn managed_mcp_present(output: &[u8]) -> bool {
    let output = String::from_utf8_lossy(output);
    output.lines().any(|line| {
        let trimmed = line.trim_start();
        let trimmed = trimmed.trim_start_matches(['-', '*', '+']).trim_start();
        let name = trimmed
            .split(|character: char| {
                character == ':'
                    || character == '('
                    || character.is_ascii_whitespace()
                    || character == '\"'
            })
            .next()
            .unwrap_or_default();
        name.eq_ignore_ascii_case(MANAGED_SERVER_NAME)
            || trimmed.contains(&format!("\"{}\"", MANAGED_SERVER_NAME))
    })
}

fn inspect_managed_mcp_with<Run>(
    cli_path: &Path,
    run: &mut Run,
) -> Result<ClaudeManagedMcpStatus, ClaudeMcpError>
where
    Run: FnMut(&Path, &[String]) -> Result<ClaudeMcpCommandOutput, ClaudeMcpError>,
{
    let args = ClaudeMcpOperation::List.args(None)?;
    let output = run(cli_path, &args)?;
    if !output.success {
        return Err(ClaudeMcpError::CommandFailed);
    }
    let configured = managed_mcp_present(&output.stdout);
    let status = if configured {
        managed_status(true, true, "configured", "受管 Claude Code MCP 已配置。")
    } else {
        managed_status(
            true,
            false,
            "not_configured",
            "受管 Claude Code MCP 尚未配置。",
        )
    };
    Ok(status)
}

fn configure_managed_mcp_with<Run>(
    cli_path: &Path,
    remote_url: &str,
    run: &mut Run,
) -> Result<ClaudeManagedMcpMutationResult, ClaudeMcpError>
where
    Run: FnMut(&Path, &[String]) -> Result<ClaudeMcpCommandOutput, ClaudeMcpError>,
{
    let remote_url = validate_remote_mcp_url(remote_url)?;
    let args = ClaudeMcpOperation::Add.args(Some(&remote_url))?;
    let output = run(cli_path, &args)?;
    if !output.success {
        return Err(ClaudeMcpError::CommandFailed);
    }
    Ok(ClaudeManagedMcpMutationResult {
        ok: true,
        state: "configured".to_string(),
        message: "受管 Claude Code MCP 已配置。".to_string(),
    })
}

fn remove_managed_mcp_with<Run>(
    cli_path: &Path,
    run: &mut Run,
) -> Result<ClaudeManagedMcpMutationResult, ClaudeMcpError>
where
    Run: FnMut(&Path, &[String]) -> Result<ClaudeMcpCommandOutput, ClaudeMcpError>,
{
    let args = ClaudeMcpOperation::Remove.args(None)?;
    let output = run(cli_path, &args)?;
    if !output.success {
        return Err(ClaudeMcpError::CommandFailed);
    }
    Ok(ClaudeManagedMcpMutationResult {
        ok: true,
        state: "not_configured".to_string(),
        message: "受管 Claude Code MCP 已移除。".to_string(),
    })
}

pub fn managed_mcp_status() -> ClaudeManagedMcpStatus {
    let cli_path = match discover_claude_cli() {
        Ok(cli_path) => cli_path,
        Err(_) => return managed_status(false, false, "cli_unavailable", CLI_UNAVAILABLE_MESSAGE),
    };
    match inspect_managed_mcp_with(&cli_path, &mut run_system_claude_cli) {
        Ok(status) => status,
        Err(_) => managed_status(true, false, "unable_to_verify", STATUS_UNKNOWN_MESSAGE),
    }
}

pub fn configure_managed_http_mcp(
    remote_url: String,
) -> Result<ClaudeManagedMcpMutationResult, String> {
    let cli_path = discover_claude_cli()
        .map_err(|error| error.public_message(ClaudeMcpOperation::Add).to_string())?;
    configure_managed_mcp_with(&cli_path, &remote_url, &mut run_system_claude_cli)
        .map_err(|error| error.public_message(ClaudeMcpOperation::Add).to_string())
}

pub fn remove_managed_mcp() -> Result<ClaudeManagedMcpMutationResult, String> {
    let cli_path = discover_claude_cli()
        .map_err(|error| error.public_message(ClaudeMcpOperation::Remove).to_string())?;
    remove_managed_mcp_with(&cli_path, &mut run_system_claude_cli)
        .map_err(|error| error.public_message(ClaudeMcpOperation::Remove).to_string())
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::fs;
    use std::os::unix::fs::PermissionsExt;
    use std::sync::atomic::{AtomicUsize, Ordering};

    static FIXTURE_SEQUENCE: AtomicUsize = AtomicUsize::new(0);

    struct ClaudeCliFixture {
        root: PathBuf,
        binary: PathBuf,
        args_path: PathBuf,
    }

    impl ClaudeCliFixture {
        fn new(list_output: &str, mcp_exit_code: i32, stderr: &str) -> Self {
            let sequence = FIXTURE_SEQUENCE.fetch_add(1, Ordering::Relaxed);
            let root = std::env::temp_dir().join(format!(
                "xiass-claude-mcp-fixture-{}-{}",
                std::process::id(),
                sequence
            ));
            fs::create_dir_all(&root).expect("fixture directory");
            let binary = root.join("claude");
            let args_path = PathBuf::from(format!("{}.args", binary.display()));
            let stdout_path = PathBuf::from(format!("{}.stdout", binary.display()));
            let stderr_path = PathBuf::from(format!("{}.stderr", binary.display()));
            fs::write(&stdout_path, list_output).expect("fixture stdout");
            fs::write(&stderr_path, stderr).expect("fixture stderr");
            let script = format!(
                "#!/bin/sh\n\
                 if [ \"$1\" = \"--version\" ]; then\n\
                   printf '%s\\n' 'Claude Code fixture'\n\
                   exit 0\n\
                 fi\n\
                 : > '{}.args'\n\
                 for value in \"$@\"; do\n\
                   printf '%s\\n' \"$value\" >> '{}.args'\n\
                 done\n\
                 cat '{}.stdout'\n\
                 cat '{}.stderr' >&2\n\
                 exit {}\n",
                binary.display(),
                binary.display(),
                binary.display(),
                binary.display(),
                mcp_exit_code
            );
            fs::write(&binary, script).expect("fixture executable");
            let mut permissions = fs::metadata(&binary)
                .expect("fixture metadata")
                .permissions();
            permissions.set_mode(0o700);
            fs::set_permissions(&binary, permissions).expect("fixture permissions");
            Self {
                root,
                binary,
                args_path,
            }
        }

        fn command_args(&self) -> Vec<String> {
            fs::read_to_string(&self.args_path)
                .expect("fixture args")
                .lines()
                .map(str::to_string)
                .collect()
        }
    }

    impl Drop for ClaudeCliFixture {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.root);
        }
    }

    fn fixture_runner(
        cli_path: &Path,
        args: &[String],
    ) -> Result<ClaudeMcpCommandOutput, ClaudeMcpError> {
        run_system_claude_cli(cli_path, args)
    }

    #[test]
    fn managed_list_uses_cli_and_hides_every_non_managed_entry() {
        let secret = "secret-list-token-must-not-escape";
        let fixture = ClaudeCliFixture::new(
            &format!(
                "unrelated-server: https://third.example/mcp?token={secret}\nxiass-tools: https://managed.example/mcp\n"
            ),
            0,
            "",
        );
        let status =
            inspect_managed_mcp_with(&fixture.binary, &mut fixture_runner).expect("managed status");

        assert!(status.cli_available);
        assert!(status.managed_server_configured);
        assert_eq!(status.state, "configured");
        assert_eq!(fixture.command_args(), vec!["mcp", "list"]);

        let serialized = serde_json::to_string(&status).expect("status json");
        assert!(!serialized.contains("unrelated-server"));
        assert!(!serialized.contains("managed.example"));
        assert!(!serialized.contains(secret));
    }

    #[test]
    fn add_uses_fixed_http_transport_and_redacts_remote_endpoint() {
        let remote_url = "https://mcp.example.test/claude";
        let fixture = ClaudeCliFixture::new("", 0, "");
        let result = configure_managed_mcp_with(&fixture.binary, remote_url, &mut fixture_runner)
            .expect("configured result");

        assert_eq!(
            fixture.command_args(),
            vec![
                "mcp",
                "add",
                "--transport",
                "http",
                "--scope",
                "user",
                MANAGED_SERVER_NAME,
                remote_url,
            ]
        );
        let serialized = serde_json::to_string(&result).expect("result json");
        assert!(result.ok);
        assert_eq!(result.state, "configured");
        assert!(!serialized.contains("mcp.example.test"));
    }

    #[test]
    fn remove_targets_only_the_xiass_managed_entry() {
        let fixture = ClaudeCliFixture::new("other-server: preserved", 0, "");
        let result =
            remove_managed_mcp_with(&fixture.binary, &mut fixture_runner).expect("removed result");

        assert!(result.ok);
        assert_eq!(result.state, "not_configured");
        assert_eq!(
            fixture.command_args(),
            vec!["mcp", "remove", MANAGED_SERVER_NAME]
        );
    }

    #[test]
    fn command_failure_is_generic_and_never_echoes_cli_output() {
        let secret = "secret-command-output-must-not-escape";
        let fixture = ClaudeCliFixture::new("", 17, secret);
        let error = configure_managed_mcp_with(
            &fixture.binary,
            "https://mcp.example.test/claude",
            &mut fixture_runner,
        )
        .expect_err("fixture should fail");

        let public_message = error.public_message(ClaudeMcpOperation::Add);
        assert_eq!(public_message, ADD_FAILED_MESSAGE);
        assert!(!public_message.contains(secret));
        assert!(!public_message.contains("mcp.example.test"));
    }

    #[test]
    fn invalid_or_credential_bearing_urls_do_not_run_the_cli() {
        let mut calls = 0usize;
        let mut runner =
            |_path: &Path, _args: &[String]| -> Result<ClaudeMcpCommandOutput, ClaudeMcpError> {
                calls += 1;
                unreachable!("invalid input must be rejected before invoking the CLI");
            };
        let error = configure_managed_mcp_with(
            Path::new("fixture"),
            "https://user:token@mcp.example.test/claude?token=secret",
            &mut runner,
        )
        .expect_err("credential URL should fail");

        assert_eq!(error, ClaudeMcpError::InvalidRemoteUrl);
        assert_eq!(calls, 0);
    }

    #[test]
    fn implementation_does_not_depend_on_legacy_claude_config_json() {
        let legacy_file_name = [".claude", ".json"].concat();
        assert!(!include_str!("claude_mcp.rs").contains(&legacy_file_name));
    }
}
