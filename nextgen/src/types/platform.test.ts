import assert from 'node:assert/strict';
import test from 'node:test';

import {
  MENU_HIDDEN_PLATFORM_IDS,
  MENU_VISIBLE_PLATFORM_IDS,
  isMenuVisiblePlatform,
} from './platform.ts';

test('exposes only the five XIASS-supported products in navigation', () => {
  assert.deepEqual(MENU_VISIBLE_PLATFORM_IDS, [
    'antigravity',
    'codex',
    'claude_manager',
    'cursor',
    'windsurf',
  ]);
  assert.ok(MENU_HIDDEN_PLATFORM_IDS.length > 0);
  assert.ok(MENU_VISIBLE_PLATFORM_IDS.every(isMenuVisiblePlatform));
  assert.equal(isMenuVisiblePlatform('codebuddy'), false);
  assert.equal(isMenuVisiblePlatform('trae'), false);
  assert.equal(isMenuVisiblePlatform('antigravity_ide'), false);
  assert.equal(isMenuVisiblePlatform('codex_api_service'), false);
});
