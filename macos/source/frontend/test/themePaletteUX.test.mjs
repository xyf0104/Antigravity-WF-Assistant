import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const globalStyles = await readFile(new URL("../src/style/global.css", import.meta.url), "utf8");

test("dark theme keeps the XIASS translucent teal surfaces and orange action language", () => {
  assert.match(globalStyles, /--bg-base: rgba\(4, 25, 33, 0\.56\);/);
  assert.match(globalStyles, /--bg-sidebar: rgba\(7, 31, 41, 0\.68\);/);
  assert.match(globalStyles, /--bg-card: rgba\(12, 43, 55, 0\.72\);/);
  assert.match(globalStyles, /--bg-inset: rgba\(3, 24, 33, 0\.7\);/);
  assert.match(globalStyles, /--accent: #f59b48;/);
  assert.match(globalStyles, /--blue: #5da5ff;/);
  assert.match(globalStyles, /--green: #38d77a;/);
});

test("dark theme secondary copy remains legible on high-density displays", () => {
  assert.match(globalStyles, /--text-secondary: rgba\(232, 239, 246, 0\.9\);/);
  assert.match(globalStyles, /--text-tertiary: rgba\(215, 226, 235, 0\.78\);/);
  assert.match(globalStyles, /--text-quaternary: rgba\(203, 218, 229, 0\.64\);/);
});

test("light theme keeps navigation and supporting copy visibly darker than separators", () => {
  assert.match(globalStyles, /--text-secondary: rgba\(27, 43, 66, 0\.9\);/);
  assert.match(globalStyles, /--text-tertiary: rgba\(38, 55, 80, 0\.78\);/);
  assert.match(globalStyles, /--text-quaternary: rgba\(48, 66, 92, 0\.64\);/);
});
