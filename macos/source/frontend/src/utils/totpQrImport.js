import jsQR from "jsqr";
import { Root } from "protobufjs/light.js";

const migrationPayloadType = Root.fromJSON({
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
        counter: { type: "int64", id: 7 },
      },
    },
  },
}).lookupType("MigrationPayload");

function decodeBase64Url(value) {
  try {
    const normalized = String(value || "").replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
    const binary = atob(padded);
    return Uint8Array.from(binary, (character) => character.charCodeAt(0));
  } catch {
    return null;
  }
}

function encodeBase32(bytes) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let buffer = 0;
  let bits = 0;
  let result = "";

  for (const byte of bytes) {
    buffer = (buffer << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      result += alphabet[(buffer >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) result += alphabet[(buffer << (5 - bits)) & 31];
  return result;
}

function supportedBase32(value) {
  const normalized = String(value || "").trim().replace(/[\s-]/g, "").toUpperCase();
  return /^[A-Z2-7]+=*$/.test(normalized) ? normalized : "";
}

function migrationAlgorithm(value) {
  switch (Number(value)) {
    case 2: return "SHA256";
    case 3: return "SHA512";
    case 1:
    default: return "SHA1";
  }
}

function migrationDigits(value) {
  return Number(value) === 2 ? 8 : 6;
}

function migrationCredential(item) {
  if (!item || Number(item.type) !== 2 || !item.secret?.length) return null;
  const secret = supportedBase32(encodeBase32(item.secret));
  if (!secret) return null;
  const issuer = String(item.issuer || "").trim();
  const account = String(item.name || "").trim();
  return {
    secret,
    issuer,
    account,
    label: [issuer, account].filter(Boolean).join(issuer && account ? ":" : "") || "Google Authenticator",
    algorithm: migrationAlgorithm(item.algorithm),
    digits: migrationDigits(item.digits),
    period: 30,
  };
}

export function parseTOTPQrPayload(raw) {
  const value = String(raw || "").trim();
  if (!value) return null;
  if (value.startsWith("otpauth://")) {
    try {
      const uri = new URL(value);
      if (uri.protocol !== "otpauth:" || uri.hostname.toLowerCase() !== "totp") return null;
      return { kind: "uri", uri: value };
    } catch {
      return null;
    }
  }
  if (!value.startsWith("otpauth-migration://")) return null;

  try {
    const uri = new URL(value);
    if (uri.protocol !== "otpauth-migration:" || uri.hostname !== "offline") return null;
    const encoded = uri.searchParams.get("data");
    const bytes = encoded ? decodeBase64Url(encoded) : null;
    if (!bytes) return null;
    const decoded = migrationPayloadType.decode(bytes);
    const credentials = (decoded.otpParameters || [])
      .map(migrationCredential)
      .filter(Boolean);
    if (!credentials.length) return null;
    return {
      kind: "migration",
      batch: {
        batchId: Number(decoded.batchId) || 0,
        batchIndex: Math.max(0, Number(decoded.batchIndex) || 0),
        batchSize: Math.max(1, Number(decoded.batchSize) || 1),
        credentials,
      },
    };
  } catch {
    return null;
  }
}

export function mergeTOTPQrMigrationBatch(current, next) {
  const parts = new Map(current?.parts || []);
  parts.set(next.batchIndex, next.credentials);
  return {
    batchId: next.batchId,
    batchSize: Math.max(current?.batchSize || 0, next.batchSize || 1),
    parts: Array.from(parts.entries()).sort(([left], [right]) => left - right),
  };
}

export function migrationBatchCredentials(batch) {
  const unique = new Map();
  for (const [, credentials] of batch?.parts || []) {
    for (const credential of credentials || []) {
      if (credential?.secret && !unique.has(credential.secret)) {
        unique.set(credential.secret, credential);
      }
    }
  }
  return Array.from(unique.values());
}

export async function decodeTOTPQrImage(imageBlob) {
  const imageUrl = URL.createObjectURL(imageBlob);
  try {
    const image = await new Promise((resolve, reject) => {
      const element = new Image();
      element.onload = () => resolve(element);
      element.onerror = reject;
      element.src = imageUrl;
    });
    const maxSide = 2200;
    const scale = Math.min(1, maxSide / Math.max(image.naturalWidth, image.naturalHeight));
    const width = Math.max(1, Math.round(image.naturalWidth * scale));
    const height = Math.max(1, Math.round(image.naturalHeight * scale));
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) return null;
    context.drawImage(image, 0, 0, width, height);
    const source = context.getImageData(0, 0, width, height);
    return jsQR(source.data, source.width, source.height, { inversionAttempts: "attemptBoth" })?.data?.trim() || null;
  } finally {
    URL.revokeObjectURL(imageUrl);
  }
}
