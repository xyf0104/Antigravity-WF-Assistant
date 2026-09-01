import test from 'node:test';
import assert from 'node:assert/strict';
import {
  XIASS_ACCOUNT_TRANSFER_SCHEMA,
  XIASS_DATA_TRANSFER_SCHEMA,
  isSupportedAccountTransferSchema,
  isSupportedDataTransferSchema,
} from './transferSchemas.ts';

test('new transfer artifacts are branded as XIASS', () => {
  assert.equal(XIASS_ACCOUNT_TRANSFER_SCHEMA, 'xiass-tools.account-transfer');
  assert.equal(XIASS_DATA_TRANSFER_SCHEMA, 'xiass-tools.data-transfer');
});

test('legacy Cockpit transfer artifacts remain import-compatible only', () => {
  assert.equal(isSupportedAccountTransferSchema('cockpit-tools.account-transfer'), true);
  assert.equal(isSupportedDataTransferSchema('cockpit-tools.data-transfer'), true);
  assert.equal(isSupportedAccountTransferSchema('unknown.account-transfer'), false);
  assert.equal(isSupportedDataTransferSchema('unknown.data-transfer'), false);
  assert.equal(isSupportedAccountTransferSchema(null), false);
  assert.equal(isSupportedDataTransferSchema({}), false);
});
