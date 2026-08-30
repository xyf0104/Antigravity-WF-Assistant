use crate::modules::wf_bridge::{self, WfBridgeSession, WfBridgeStatus};

#[tauri::command]
pub async fn wf_bridge_get_session() -> Result<WfBridgeSession, String> {
    tauri::async_runtime::spawn_blocking(wf_bridge::get_or_start_session)
        .await
        .map_err(|error| format!("启动 WF 原生组件任务失败：{error}"))?
}

#[tauri::command]
pub fn wf_bridge_get_status() -> Result<WfBridgeStatus, String> {
    wf_bridge::get_status()
}

#[tauri::command]
pub async fn wf_bridge_stop() -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(wf_bridge::stop)
        .await
        .map_err(|error| format!("停止 WF 原生组件任务失败：{error}"))?
}
