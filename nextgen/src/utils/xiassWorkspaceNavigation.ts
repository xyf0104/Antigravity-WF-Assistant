export const XIASS_WORKSPACE_PANEL_NAVIGATE_EVENT =
  'xiass-tools:workspace-panel-navigate';

export type XiassWorkspacePanelPlatform =
  | 'codex'
  | 'claude-code'
  | 'cursor'
  | 'windsurf';

export interface XiassWorkspacePanelNavigateDetail {
  platform: XiassWorkspacePanelPlatform;
  tab: string;
}

export function navigateXiassWorkspacePanel(
  detail: XiassWorkspacePanelNavigateDetail,
): void {
  window.dispatchEvent(
    new CustomEvent<XiassWorkspacePanelNavigateDetail>(
      XIASS_WORKSPACE_PANEL_NAVIGATE_EVENT,
      { detail },
    ),
  );
}
