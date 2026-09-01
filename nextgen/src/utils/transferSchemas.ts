/**
 * Public transfer files are XIASS artifacts. Legacy Cockpit schemas remain
 * import-only so an existing backup can always be restored after upgrading.
 */
export const XIASS_ACCOUNT_TRANSFER_SCHEMA = 'xiass-tools.account-transfer';
export const XIASS_DATA_TRANSFER_SCHEMA = 'xiass-tools.data-transfer';

const LEGACY_ACCOUNT_TRANSFER_SCHEMAS = new Set([
  'cockpit-tools.account-transfer',
]);

const LEGACY_DATA_TRANSFER_SCHEMAS = new Set([
  'cockpit-tools.data-transfer',
]);

export function isSupportedAccountTransferSchema(value: unknown): boolean {
  return value === XIASS_ACCOUNT_TRANSFER_SCHEMA
    || (typeof value === 'string' && LEGACY_ACCOUNT_TRANSFER_SCHEMAS.has(value));
}

export function isSupportedDataTransferSchema(value: unknown): boolean {
  return value === XIASS_DATA_TRANSFER_SCHEMA
    || (typeof value === 'string' && LEGACY_DATA_TRANSFER_SCHEMAS.has(value));
}
