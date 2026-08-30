import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const accountsSource = await readFile(new URL("../src/views/Accounts.vue", import.meta.url), "utf8");
const modalSource = await readFile(new URL("../src/components/accounts/AccountTestModal.vue", import.meta.url), "utf8");
const quotaSource = await readFile(new URL("../src/components/accounts/AccountQuotaWindows.vue", import.meta.url), "utf8");

test("account test preserves typed completion state and never renders the same completion twice", () => {
  assert.match(accountsSource, /type: readText\(step\?\.type\)/);
  assert.match(modalSource, /const hasCompletionStep = computed/);
  assert.match(modalSource, /line\.type === "complete"/);
  assert.match(modalSource, /status === 'success' && !hasCompletionStep/);
  assert.doesNotMatch(modalSource, /v-if="status === 'success'" class="terminal-result/);
});

test("account-card probes hide the editable prompt but retain the image probe default", () => {
  assert.match(accountsSource, /<AccountTestModal[\s\S]*?:show-prompt="false"/);
  assert.match(modalSource, /function normalizePrompt/);
  assert.match(modalSource, /DEFAULT_IMAGE_PROMPT/);
  assert.match(modalSource, /prompt: testPrompt\.value\.trim\(\) \|\| "hi"/);
});

test("quota actions have an implemented local usage-statistics route", () => {
  assert.match(quotaSource, /defineEmits\(\["refresh", "view-requests"\]\)/);
  assert.doesNotMatch(quotaSource, /view-details/);
  assert.match(quotaSource, /emit\('view-requests'\)/);
  assert.match(quotaSource, /◌ 本机统计/);
  assert.match(accountsSource, /@view-requests="openAccountUsageDetails\(account\)"/);
  assert.match(accountsSource, /const accountUsageDetailsAccount = ref\(null\)/);
  assert.match(accountsSource, /title="本机转发统计"/);
  assert.match(accountsSource, /不会保存或展示请求文本、文件、图片、凭据或聊天内容/);
});
