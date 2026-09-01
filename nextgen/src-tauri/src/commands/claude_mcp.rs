//! Tauri command surface for the managed Claude Code MCP adapter.

use crate::modules::claude_mcp::{self, ClaudeManagedMcpMutationResult, ClaudeManagedMcpStatus};

#[tauri::command]
pub fn claude_mcp_get_managed_status() -> ClaudeManagedMcpStatus {
    claude_mcp::managed_mcp_status()
}

#[tauri::command]
pub fn claude_mcp_configure_managed_http(
    remote_url: String,
) -> Result<ClaudeManagedMcpMutationResult, String> {
    claude_mcp::configure_managed_http_mcp(remote_url)
}

#[tauri::command]
pub fn claude_mcp_remove_managed() -> Result<ClaudeManagedMcpMutationResult, String> {
    claude_mcp::remove_managed_mcp()
}
