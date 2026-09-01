import assert from "node:assert/strict";
import test from "node:test";
import { Root } from "protobufjs/light.js";
import {
  mergeTOTPQrMigrationBatch,
  migrationBatchCredentials,
  parseTOTPQrPayload,
} from "../src/utils/totpQrImport.js";

const payloadType = Root.fromJSON({
  nested: {
    MigrationPayload: {
      fields: {
        otpParameters: { rule: "repeated", type: "OtpParameters", id: 1 },
        version: { type: "int32", id: 2 },
        batchSize: { type: "int32", id: 3 },
        batchIndex: { type: "int32", id: 4 },
        batchId: { type: "int32", id: 5 },
      },
    },
    OtpParameters: {
      fields: {
        secret: { type: "bytes", id: 1 },
        name: { type: "string", id: 2 },
        issuer: { type: "string", id: 3 },
        algorithm: { type: "int32", id: 4 },
        digits: { type: "int32", id: 5 },
        type: { type: "int32", id: 6 },
      },
    },
  },
}).lookupType("MigrationPayload");

function migrationUri({ batchId = 7, batchIndex = 0, batchSize = 1, entries = [] } = {}) {
  const bytes = payloadType.encode({
    version: 1,
    batchId,
    batchIndex,
    batchSize,
    otpParameters: entries,
  }).finish();
  return `otpauth-migration://offline?data=${Buffer.from(bytes).toString("base64url")}`;
}

test("accepts a standard TOTP URI without persisting it", () => {
  const uri = "otpauth://totp/XIASS%3Aalice?secret=JBSWY3DPEHPK3PXP&issuer=XIASS";
  assert.deepEqual(parseTOTPQrPayload(uri), { kind: "uri", uri });
});

test("parses a Google Authenticator migration batch into TOTP-only credentials", () => {
  const parsed = parseTOTPQrPayload(migrationUri({
    batchId: 24,
    batchIndex: 1,
    batchSize: 3,
    entries: [
      {
        secret: Uint8Array.from([1, 2, 3, 4, 5]),
        name: "alice@example.com",
        issuer: "XIASS",
        algorithm: 2,
        digits: 2,
        type: 2,
      },
      {
        secret: Uint8Array.from([6, 7, 8]),
        name: "counter-based",
        issuer: "XIASS",
        type: 1,
      },
    ],
  }));

  assert.equal(parsed?.kind, "migration");
  assert.deepEqual(parsed?.batch, {
    batchId: 24,
    batchIndex: 1,
    batchSize: 3,
    credentials: [{
      secret: "AEBAGBAF",
      issuer: "XIASS",
      account: "alice@example.com",
      label: "XIASS:alice@example.com",
      algorithm: "SHA256",
      digits: 8,
      period: 30,
    }],
  });
});

test("rejects malformed, unsupported, and non-TOTP migration data", () => {
  assert.equal(parseTOTPQrPayload(""), null);
  assert.equal(parseTOTPQrPayload("https://example.test/qr"), null);
  assert.equal(parseTOTPQrPayload("otpauth://hotp/XIASS?secret=JBSWY3DPEHPK3PXP"), null);
  assert.equal(parseTOTPQrPayload("otpauth-migration://offline?data=not-base64"), null);
  assert.equal(parseTOTPQrPayload(migrationUri({
    entries: [{ secret: Uint8Array.from([1, 2, 3]), type: 1 }],
  })), null);
});

test("merges unordered migration QR parts and deduplicates credentials", () => {
  const second = parseTOTPQrPayload(migrationUri({
    batchId: 88,
    batchIndex: 1,
    batchSize: 2,
    entries: [{ secret: Uint8Array.from([9, 9, 9]), name: "second", type: 2 }],
  }));
  const first = parseTOTPQrPayload(migrationUri({
    batchId: 88,
    batchIndex: 0,
    batchSize: 2,
    entries: [
      { secret: Uint8Array.from([1, 2, 3]), name: "first", type: 2 },
      { secret: Uint8Array.from([9, 9, 9]), name: "second copy", type: 2 },
    ],
  }));

  assert.equal(second?.kind, "migration");
  assert.equal(first?.kind, "migration");
  const batch = mergeTOTPQrMigrationBatch(
    mergeTOTPQrMigrationBatch(null, second.batch),
    first.batch,
  );

  assert.deepEqual(batch.parts.map(([index]) => index), [0, 1]);
  assert.equal(batch.batchSize, 2);
  assert.deepEqual(
    migrationBatchCredentials(batch).map((credential) => credential.account),
    ["first", "second copy"],
  );
});
