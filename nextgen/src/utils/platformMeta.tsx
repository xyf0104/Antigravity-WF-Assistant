import { ReactNode } from 'react';
import { TFunction } from 'i18next';
import { PlatformId } from '../types/platform';
import { AntigravityIcon } from '../components/icons/AntigravityIcon';
import { AntigravityIdeIcon } from '../components/icons/AntigravityIdeIcon';
import { CodexIcon } from '../components/icons/CodexIcon';
import { ClaudeIcon } from '../components/icons/ClaudeIcon';
import { WindsurfIcon } from '../components/icons/WindsurfIcon';
import { CursorIcon } from '../components/icons/CursorIcon';
export function getPlatformLabel(platformId: PlatformId, _t: TFunction): string {
  switch (platformId) {
    case 'antigravity':
      return 'Antigravity WF';
    case 'antigravity_ide':
      return 'Antigravity WF';
    case 'codex':
      return 'Codex';
    case 'codex_api_service':
      return _t('codex.apiService.navTitle', 'Codex API Service');
    case 'claude_manager':
      return 'Claude Code';
    case 'zed':
      return 'Zed';
    case 'github-copilot':
      return 'GitHub Copilot';
    case 'windsurf':
      return 'Windsurf';
    case 'kiro':
      return 'Kiro';
    case 'cursor':
      return 'Cursor';
    case 'grok':
      return 'Grok CLI';
    case 'codebuddy':
      return 'CodeBuddy';
    case 'codebuddy_cn':
      return _t('nav.codebuddyCn', 'CodeBuddy CN');
    case 'qoder':
      return _t('nav.qoder', 'Qoder');
    case 'zcode':
      return 'ZCode';
    case 'trae':
      return _t('nav.trae', 'Trae');
    case 'trae_solo':
      return _t('nav.traeSolo', 'TRAE SOLO');
    case 'trae_cn':
      return _t('nav.traeCn', 'Trae CN');
    case 'trae_solo_cn':
      return _t('nav.traeSoloCn', 'TRAE SOLO CN');
    case 'workbuddy':
      return 'WorkBuddy';
    default:
      return platformId;
  }
}

export function renderPlatformIcon(platformId: PlatformId, size = 20): ReactNode {
  switch (platformId) {
    case 'antigravity':
      return <AntigravityIcon style={{ width: size, height: size }} />;
    case 'antigravity_ide':
      return <AntigravityIdeIcon style={{ width: size, height: size }} />;
    case 'codex':
      return <CodexIcon size={size} />;
    case 'codex_api_service':
      return <CodexIcon size={size} />;
    case 'claude_manager':
      return <ClaudeIcon size={size} />;
    case 'windsurf':
      return <WindsurfIcon style={{ width: size, height: size }} />;
    case 'cursor':
      return <CursorIcon style={{ width: size, height: size }} />;
    default:
      return null;
  }
}
