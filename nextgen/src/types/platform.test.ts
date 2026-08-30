import assert from 'node:assert/strict';
import test from 'node:test';

import {
  ALL_PLATFORM_IDS,
  MENU_HIDDEN_PLATFORM_IDS,
  MENU_VISIBLE_PLATFORM_IDS,
  isMenuVisiblePlatform,
} from './platform.ts';

test('keeps every imported Cockpit platform available in XIASS navigation', () => {
  assert.deepEqual(MENU_HIDDEN_PLATFORM_IDS, []);
  assert.deepEqual(MENU_VISIBLE_PLATFORM_IDS, ALL_PLATFORM_IDS);
  assert.ok(ALL_PLATFORM_IDS.every(isMenuVisiblePlatform));
});
