import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  accountIsEnabled,
  accountProviderFilterOptions,
  selectDirectoryAccounts,
} from "../src/state/accountDirectory.js";

const accountsSource = await readFile(new URL("../src/views/Accounts.vue", import.meta.url), "utf8");

const accounts = [
  {
    id: "alpha",
    name: "Alpha 团队",
    notes: "主线路",
    provider: "openai",
    apiUrl: "https://alpha.example.test",
    type: "api_key",
    priority: 30,
    enabled: true,
    lastSuccessAt: "2026-08-29T10:00:00Z",
    identity: { email: "alpha@example.test", plan: "Pro" },
    apiKey: "secret-alpha-key",
  },
  {
    id: "bravo",
    name: "Bravo 备用",
    provider: "anthropic",
    apiUrl: "https://bravo.example.test",
    type: "oauth",
    priority: 10,
    enabled: false,
    lastSuccessAt: "2026-08-30T10:00:00Z",
    identity: { email: "bravo@example.test", plan: "Free" },
    apiKey: "secret-bravo-key",
  },
  {
    id: "charlie",
    name: "Charlie",
    provider: "grok",
    apiUrl: "https://charlie.example.test",
    type: "bearer_token",
    priority: 20,
    identity: { email: "charlie@example.test" },
    apiKey: "secret-charlie-key",
  },
];

test("account directory filters locally without exposing credential fields to search", () => {
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { search: "ALPHA", sort: "name" }).map((account) => account.id),
    ["alpha"],
  );
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { search: "bravo@example.test", sort: "name" }).map((account) => account.id),
    ["bravo"],
  );
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { search: "secret-bravo-key", sort: "name" }).map((account) => account.id),
    [],
  );
});

test("account directory filters provider and enabled state while preserving legacy accounts", () => {
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { provider: "anthropic" }).map((account) => account.id),
    ["bravo"],
  );
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { status: "enabled" }).map((account) => account.id),
    ["charlie", "alpha"],
  );
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { status: "paused" }).map((account) => account.id),
    ["bravo"],
  );
  assert.equal(accountIsEnabled({ id: "legacy" }), true);
  assert.equal(accountIsEnabled({ id: "paused", enabled: false }), false);
});

test("directory sort handles priority, latest success and account name without mutating source data", () => {
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { sort: "priority" }).map((account) => account.id),
    ["bravo", "charlie", "alpha"],
  );
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { sort: "recent_success" }).map((account) => account.id),
    ["bravo", "alpha", "charlie"],
  );
  assert.deepEqual(
    selectDirectoryAccounts(accounts, { sort: "name" }).map((account) => account.id),
    ["alpha", "bravo", "charlie"],
  );
  assert.deepEqual(accounts.map((account) => account.id), ["alpha", "bravo", "charlie"]);
});

test("provider filter choices include current known and compatibility providers", () => {
  const options = accountProviderFilterOptions([...accounts, { id: "delta", provider: "vendor-x" }]);
  assert.deepEqual(options.map((option) => option.value), ["all", "openai", "anthropic", "grok", "vendor-x"]);
  assert.equal(options.at(-1).label, "vendor-x");
});

test("accounts page provides keyboard-accessible local filters and reversible batch status actions", () => {
  assert.match(accountsSource, /import \{[\s\S]*selectDirectoryAccounts[\s\S]*\} from "@\/state\/accountDirectory"/);
  assert.match(accountsSource, /const visibleAccounts = computed\(\(\) => selectDirectoryAccounts/);
  assert.match(accountsSource, /role="search" aria-label="筛选账户"/);
  assert.match(accountsSource, /@keydown\.esc\.prevent="clearAccountSearch"/);
  assert.match(accountsSource, /aria-live="polite"/);
  assert.match(accountsSource, /:indeterminate="selectedVisibleAccountCount > 0 && !allVisibleAccountsSelected"/);
  assert.match(accountsSource, /批量启用/);
  assert.match(accountsSource, /批量暂停/);
  assert.match(accountsSource, /async function updateSelectedAccountsEnabled\(enabled\)/);
  assert.match(accountsSource, /for \(let index = 0; index < targets\.length; index \+= 1\)/);
  assert.match(accountsSource, /v-else-if="!visibleAccounts\.length" class="empty filter-empty"/);
  assert.match(accountsSource, /这些条件只在当前窗口本地生效/);
});
