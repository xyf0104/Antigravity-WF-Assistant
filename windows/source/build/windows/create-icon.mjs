// Combine PNG representations into a Windows ICO without changing their pixels.
// This keeps the installer, desktop shortcut and executable icon in sync with
// build/appicon.png; macOS's `sips` creates the size-specific PNG inputs.
import { readFileSync, writeFileSync } from "node:fs";

const [output, ...inputPaths] = process.argv.slice(2);
if (!output || inputPaths.length === 0) {
  throw new Error("Usage: node create-icon.mjs OUTPUT.ico INPUT.png [...]");
}

const pngSize = (buffer) => {
  if (buffer.toString("ascii", 1, 4) !== "PNG") throw new Error("ICO input must be PNG");
  return { width: buffer.readUInt32BE(16), height: buffer.readUInt32BE(20) };
};

const images = inputPaths.map((inputPath) => {
  const data = readFileSync(inputPath);
  return { data, ...pngSize(data) };
});

const directorySize = 6 + images.length * 16;
const header = Buffer.alloc(directorySize);
header.writeUInt16LE(0, 0);
header.writeUInt16LE(1, 2);
header.writeUInt16LE(images.length, 4);

let offset = directorySize;
images.forEach((image, index) => {
  const position = 6 + index * 16;
  header.writeUInt8(image.width === 256 ? 0 : image.width, position);
  header.writeUInt8(image.height === 256 ? 0 : image.height, position + 1);
  header.writeUInt8(0, position + 2);
  header.writeUInt8(0, position + 3);
  header.writeUInt16LE(1, position + 4);
  header.writeUInt16LE(32, position + 6);
  header.writeUInt32LE(image.data.length, position + 8);
  header.writeUInt32LE(offset, position + 12);
  offset += image.data.length;
});

writeFileSync(output, Buffer.concat([header, ...images.map(({ data }) => data)]));
