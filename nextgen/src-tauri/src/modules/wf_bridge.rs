use rand::{distributions::Alphanumeric, Rng};
use serde::{Deserialize, Serialize};
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::{mpsc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use crate::modules::logger;

const WF_BRIDGE_BIN_NAME: &str = "xiass-wf-bridge";
const WF_BRIDGE_START_TIMEOUT: Duration = Duration::from_secs(15);

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WfBridgeSession {
    pub url: String,
    pub token: String,
    pub host: String,
    pub port: u16,
    pub schema_version: u32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WfBridgeStatus {
    pub running: bool,
    pub url: Option<String>,
    pub last_error: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct WfBridgeReadySignal {
    event: String,
    service: String,
    host: String,
    port: u16,
    #[serde(default)]
    schema_version: u32,
}

#[derive(Default)]
struct WfBridgeRuntime {
    child: Option<Child>,
    session: Option<WfBridgeSession>,
    last_error: Option<String>,
}

static WF_BRIDGE_RUNTIME: OnceLock<Mutex<WfBridgeRuntime>> = OnceLock::new();

fn runtime() -> &'static Mutex<WfBridgeRuntime> {
    WF_BRIDGE_RUNTIME.get_or_init(|| Mutex::new(WfBridgeRuntime::default()))
}

fn bridge_binary_file_names() -> Vec<String> {
    let target = env!("XIASS_RUST_TARGET");
    if cfg!(target_os = "windows") {
        vec![
            format!("{WF_BRIDGE_BIN_NAME}.exe"),
            format!("{WF_BRIDGE_BIN_NAME}-{target}.exe"),
        ]
    } else {
        vec![
            WF_BRIDGE_BIN_NAME.to_string(),
            format!("{WF_BRIDGE_BIN_NAME}-{target}"),
        ]
    }
}

fn push_binary_candidates(candidates: &mut Vec<PathBuf>, directory: &Path) {
    for name in bridge_binary_file_names() {
        let path = directory.join(name);
        if !candidates.iter().any(|candidate| candidate == &path) {
            candidates.push(path);
        }
    }
}

fn bridge_binary_candidates() -> Result<Vec<PathBuf>, String> {
    let executable = std::env::current_exe()
        .map_err(|error| format!("读取 XIASS Tools 程序路径失败：{error}"))?;
    let executable_dir = executable
        .parent()
        .ok_or_else(|| format!("XIASS Tools 程序路径缺少父目录：{}", executable.display()))?;
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let development_dir = manifest_dir.join("../sidecars/wf-bridge/bin");
    let mut candidates = Vec::new();

    if cfg!(debug_assertions) {
        push_binary_candidates(&mut candidates, &development_dir);
    }
    push_binary_candidates(&mut candidates, executable_dir);
    if let Some(contents_dir) = executable_dir.parent() {
        push_binary_candidates(&mut candidates, &contents_dir.join("Resources"));
    }
    if !cfg!(debug_assertions) {
        push_binary_candidates(&mut candidates, &development_dir);
    }
    Ok(candidates)
}

fn bridge_binary_path() -> Result<PathBuf, String> {
    let candidates = bridge_binary_candidates()?;
    candidates
        .iter()
        .find(|candidate| candidate.is_file())
        .cloned()
        .ok_or_else(|| {
            format!(
                "WF 原生组件不存在；请重新安装 XIASS Tools。已检查：{}",
                candidates
                    .iter()
                    .map(|candidate| candidate.display().to_string())
                    .collect::<Vec<_>>()
                    .join("、")
            )
        })
}

fn sanitize_bridge_environment(command: &mut Command) {
    #[cfg(target_os = "macos")]
    {
        command.env_remove("__CFBundleIdentifier");
        command.env_remove("XPC_SERVICE_NAME");
    }
    #[cfg(not(target_os = "macos"))]
    let _ = command;
}

fn refresh_process_status(runtime: &mut WfBridgeRuntime) {
    let Some(child) = runtime.child.as_mut() else {
        runtime.session = None;
        return;
    };
    match child.try_wait() {
        Ok(Some(status)) => {
            runtime.last_error = Some(format!("WF 原生组件已退出：{status}"));
            runtime.child = None;
            runtime.session = None;
        }
        Ok(None) => {}
        Err(error) => {
            runtime.last_error = Some(format!("检查 WF 原生组件状态失败：{error}"));
            runtime.child = None;
            runtime.session = None;
        }
    }
}

fn redact_log_line(line: &str) -> String {
    let line = line.trim();
    if line.len() <= 2_000 {
        line.to_string()
    } else {
        format!("{}…", line.chars().take(2_000).collect::<String>())
    }
}

fn spawn_stdout_reader(stdout: std::process::ChildStdout, sender: mpsc::Sender<String>) {
    std::thread::spawn(move || {
        for line in BufReader::new(stdout).lines() {
            match line {
                Ok(line) => {
                    let _ = sender.send(line);
                }
                Err(error) => {
                    logger::log_warn(&format!("[WFBridge] 读取组件输出失败：{error}"));
                    break;
                }
            }
        }
    });
}

fn spawn_stderr_reader(stderr: std::process::ChildStderr) {
    std::thread::spawn(move || {
        for line in BufReader::new(stderr).lines() {
            match line {
                Ok(line) if !line.trim().is_empty() => {
                    logger::log_warn(&format!("[WFBridge] {}", redact_log_line(&line)))
                }
                Ok(_) => {}
                Err(error) => {
                    logger::log_warn(&format!("[WFBridge] 读取组件诊断失败：{error}"));
                    break;
                }
            }
        }
    });
}

fn parse_ready_signal(line: &str) -> Option<WfBridgeReadySignal> {
    let signal = serde_json::from_str::<WfBridgeReadySignal>(line.trim()).ok()?;
    if signal.event != "ready" || signal.service != WF_BRIDGE_BIN_NAME {
        return None;
    }
    Some(signal)
}

fn verify_ready_signal(signal: &WfBridgeReadySignal) -> Result<(), String> {
    if signal.host != "127.0.0.1" {
        return Err("WF 原生组件拒绝了非本机监听地址".to_string());
    }
    if signal.port == 0 {
        return Err("WF 原生组件返回了无效端口".to_string());
    }
    if signal.schema_version != 1 {
        return Err(format!(
            "WF 原生组件协议版本不兼容：{}",
            signal.schema_version
        ));
    }
    Ok(())
}

fn probe_health(url: &str) -> Result<(), String> {
    let client = reqwest::blocking::Client::builder()
        .timeout(Duration::from_secs(3))
        .no_proxy()
        .build()
        .map_err(|error| format!("创建 WF 本机健康检查失败：{error}"))?;
    let response = client
        .get(format!("{url}/health"))
        .send()
        .map_err(|error| format!("WF 原生组件健康检查失败：{error}"))?;
    if !response.status().is_success() {
        return Err(format!("WF 原生组件健康检查返回 {}", response.status()));
    }
    Ok(())
}

fn stop_child(child: &mut Child) {
    let _ = child.kill();
    let _ = child.wait();
}

fn start_bridge(runtime: &mut WfBridgeRuntime) -> Result<WfBridgeSession, String> {
    let binary = bridge_binary_path()?;
    let token: String = rand::thread_rng()
        .sample_iter(&Alphanumeric)
        .take(64)
        .map(char::from)
        .collect();
    let mut command = Command::new(&binary);
    sanitize_bridge_environment(&mut command);
    command
        .env("XIASS_WF_RPC_TOKEN", &token)
        .env("XIASS_WF_RPC_PORT", "0")
        .env("XIASS_PARENT_PID", std::process::id().to_string())
        // 子进程读取此匿名管道。父进程正常退出、崩溃或被强制终止时，操作系统
        // 都会关闭写端，WF bridge 随即释放 HTTP 监听和本地代理端口。
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(target_os = "windows")]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x0800_0000);
    }

    let mut child = command
        .spawn()
        .map_err(|error| format!("启动 WF 原生组件失败：{error}"))?;
    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| "WF 原生组件没有输出启动状态".to_string())?;
    if let Some(stderr) = child.stderr.take() {
        spawn_stderr_reader(stderr);
    }
    let (sender, receiver) = mpsc::channel();
    spawn_stdout_reader(stdout, sender);
    let deadline = Instant::now() + WF_BRIDGE_START_TIMEOUT;
    let ready = loop {
        if let Ok(Some(status)) = child.try_wait() {
            return Err(format!("WF 原生组件启动前已退出：{status}"));
        }
        let now = Instant::now();
        if now >= deadline {
            stop_child(&mut child);
            return Err("WF 原生组件启动超时".to_string());
        }
        match receiver.recv_timeout((deadline - now).min(Duration::from_millis(250))) {
            Ok(line) => {
                if let Some(signal) = parse_ready_signal(&line) {
                    break signal;
                }
                if !line.trim().is_empty() {
                    logger::log_info(&format!("[WFBridge] {}", redact_log_line(&line)));
                }
            }
            Err(mpsc::RecvTimeoutError::Timeout) => continue,
            Err(mpsc::RecvTimeoutError::Disconnected) => {
                stop_child(&mut child);
                return Err("WF 原生组件启动输出已关闭".to_string());
            }
        }
    };
    verify_ready_signal(&ready)?;
    let url = format!("http://{}:{}", ready.host, ready.port);
    if let Err(error) = probe_health(&url) {
        stop_child(&mut child);
        return Err(error);
    }
    let session = WfBridgeSession {
        url,
        token,
        host: ready.host,
        port: ready.port,
        schema_version: ready.schema_version,
    };
    runtime.child = Some(child);
    runtime.session = Some(session.clone());
    runtime.last_error = None;
    logger::log_info(&format!(
        "[WFBridge] 原生组件已就绪：host={} port={}",
        session.host, session.port
    ));
    Ok(session)
}

pub fn get_or_start_session() -> Result<WfBridgeSession, String> {
    let mut runtime = runtime()
        .lock()
        .map_err(|_| "WF 原生组件运行状态锁不可用".to_string())?;
    refresh_process_status(&mut runtime);
    if let Some(session) = runtime.session.clone() {
        return Ok(session);
    }
    match start_bridge(&mut runtime) {
        Ok(session) => Ok(session),
        Err(error) => {
            runtime.last_error = Some(error.clone());
            Err(error)
        }
    }
}

pub fn get_status() -> Result<WfBridgeStatus, String> {
    let mut runtime = runtime()
        .lock()
        .map_err(|_| "WF 原生组件运行状态锁不可用".to_string())?;
    refresh_process_status(&mut runtime);
    Ok(WfBridgeStatus {
        running: runtime.child.is_some(),
        url: runtime.session.as_ref().map(|session| session.url.clone()),
        last_error: runtime.last_error.clone(),
    })
}

pub fn stop() -> Result<(), String> {
    let mut runtime = runtime()
        .lock()
        .map_err(|_| "WF 原生组件运行状态锁不可用".to_string())?;
    if let Some(mut child) = runtime.child.take() {
        stop_child(&mut child);
    }
    runtime.session = None;
    runtime.last_error = None;
    logger::log_info("[WFBridge] 原生组件已停止");
    Ok(())
}

pub fn stop_for_app_exit() {
    if let Ok(mut runtime) = runtime().lock() {
        if let Some(mut child) = runtime.child.take() {
            stop_child(&mut child);
        }
        runtime.session = None;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_only_expected_ready_signal() {
        let signal = parse_ready_signal(
            r#"{"event":"ready","service":"xiass-wf-bridge","host":"127.0.0.1","port":50991,"schemaVersion":1}"#,
        )
        .expect("ready signal");
        assert!(verify_ready_signal(&signal).is_ok());
        assert!(parse_ready_signal(r#"{"event":"log"}"#).is_none());
    }

    #[test]
    fn rejects_non_loopback_ready_signal() {
        let signal = WfBridgeReadySignal {
            event: "ready".to_string(),
            service: WF_BRIDGE_BIN_NAME.to_string(),
            host: "0.0.0.0".to_string(),
            port: 50991,
            schema_version: 1,
        };
        assert!(verify_ready_signal(&signal).is_err());
    }

    #[test]
    fn candidate_names_include_current_target() {
        let names = bridge_binary_file_names();
        assert!(names
            .iter()
            .any(|name| name.contains(env!("XIASS_RUST_TARGET"))));
    }
}
