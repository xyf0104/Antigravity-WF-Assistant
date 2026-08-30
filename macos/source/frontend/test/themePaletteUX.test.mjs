import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const globalStyles = await readFile(new URL("../src/style/global.css", import.meta.url), "utf8");

test("dark theme keeps the requested navy card and orange action language", () => {
  assert.match(globalStyles, /--bg-base: #111a29;/);
  assert.match(globalStyles, /--bg-sidebar: rgba\(15, 27, 45, 0\.97\);/);
  assert.match(globalStyles, /--bg-card: rgba\(29, 44, 65, 0\.94\);/);
  assert.match(globalStyles, /--bg-inset: rgba\(10, 20, 35, 0\.72\);/);
  assert.match(globalStyles, /--accent: #f59b48;/);
  assert.match(globalStyles, /--blue: #5da5ff;/);
  assert.match(globalStyles, /--green: #38d77a;/);
});

test("dark theme secondary copy remains legible on high-density displays", () => {
  assert.match(globalStyles, /--text-secondary: rgba\(226, 232, 240, 0\.82\);/);
  assert.match(globalStyles, /--text-tertiary: rgba\(203, 213, 225, 0\.68\);/);
  assert.match(globalStyles, /--text-quaternary: rgba\(203, 213, 225, 0\.5\);/);
});

test("light theme keeps navigation and supporting copy visibly darker than separators", () => {
  assert.match(globalStyles, /--text-secondary: rgba\(32, 46, 69, 0\.82\);/);
  assert.match(globalStyles, /--text-tertiary: rgba\(43, 58, 83, 0\.68\);/);
  assert.match(globalStyles, /--text-quaternary: rgba\(55, 72, 98, 0\.5\);/);
});
