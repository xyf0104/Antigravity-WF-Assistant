// Local-only account directory helpers. These intentionally operate on the
// already-redacted account view model and never inspect credentials, headers
// or OAuth token fields. Keeping this logic pure makes sorting and filtering
// predictable in both desktop clients without changing persisted accounts.

export const ACCOUNT_STATUS_OPTIONS = [
  { value: "all", label: "全部状态" },
  { value: "enabled", label: "已启用" },
  { value: "paused", label: "已暂停" },
];

export const ACCOUNT_SORT_OPTIONS = [
  { value: "priority", label: "优先级" },
  { value: "recent_success", label: "最近成功" },
  { value: "name", label: "账户名称" },
];

const PROVIDER_LABELS = {
  openai: "OpenAI",
  anthropic: "Claude",
  grok: "Grok",
  custom: "兼容接口",
};

const PROVIDER_ORDER = ["openai", "anthropic", "grok", "custom"];
const collator = new Intl.Collator("zh-CN", { sensitivity: "base", numeric: true });

function text(value) {
  return typeof value === "string" ? value.trim() : "";
}

export function accountProviderKey(account) {
  return text(account?.provider).toLowerCase() || "openai";
}

export function accountProviderLabel(provider) {
  const key = text(provider).toLowerCase();
  if (!key) return PROVIDER_LABELS.openai;
  return PROVIDER_LABELS[key] || key;
}

// Older account records predate the explicit enabled flag. Treating those as
// enabled preserves their existing scheduling behaviour and lets users pause
// them explicitly from the directory.
export function accountIsEnabled(account) {
  return account?.enabled !== false;
}

export function accountProviderFilterOptions(accounts) {
  const keys = new Set((Array.isArray(accounts) ? accounts : []).map(accountProviderKey));
  const ordered = [
    ...PROVIDER_ORDER.filter((key) => keys.has(key)),
    ...[...keys].filter((key) => !PROVIDER_ORDER.includes(key)).sort(collator.compare),
  ];
  return [
    { value: "all", label: "全部提供商" },
    ...ordered.map((value) => ({ value, label: accountProviderLabel(value) })),
  ];
}

function accountSearchText(account) {
  const identity = account?.identity && typeof account.identity === "object" ? account.identity : {};
  // Do not add apiKey, headers, access token, refresh token or raw OAuth data
  // here. Search only covers information that is already intended for account
  // identification in this UI.
  return [
    account?.name,
    account?.notes,
    account?.apiUrl,
    accountProviderKey(account),
    account?.type,
    identity?.name,
    identity?.email,
    identity?.organization,
    identity?.plan,
  ].map(text).filter(Boolean).join(" ").toLocaleLowerCase("zh-CN");
}

function successTimestamp(account) {
  const value = account?.lastSuccessAt ?? account?.last_success_at;
  const timestamp = Date.parse(value || "");
  return Number.isFinite(timestamp) ? timestamp : Number.NEGATIVE_INFINITY;
}

function priorityValue(account) {
  const priority = Number(account?.priority);
  return Number.isFinite(priority) ? priority : 50;
}

function displayName(account) {
  return text(account?.name) || accountProviderLabel(accountProviderKey(account));
}

function compareName(left, right) {
  const nameOrder = collator.compare(displayName(left), displayName(right));
  if (nameOrder) return nameOrder;
  return collator.compare(text(left?.id), text(right?.id));
}

function compareAccounts(left, right, sort) {
  if (sort === "name") return compareName(left, right);
  if (sort === "recent_success") {
    const successOrder = successTimestamp(right) - successTimestamp(left);
    if (successOrder) return successOrder;
    const priorityOrder = priorityValue(left) - priorityValue(right);
    return priorityOrder || compareName(left, right);
  }
  const priorityOrder = priorityValue(left) - priorityValue(right);
  return priorityOrder || compareName(left, right);
}

export function selectDirectoryAccounts(accounts, filters = {}) {
  const list = Array.isArray(accounts) ? accounts : [];
  const query = text(filters.search).toLocaleLowerCase("zh-CN");
  const provider = text(filters.provider).toLowerCase() || "all";
  const status = text(filters.status).toLowerCase() || "all";
  const sort = text(filters.sort) || "priority";

  return list
    .filter((account) => {
      if (query && !accountSearchText(account).includes(query)) return false;
      if (provider !== "all" && accountProviderKey(account) !== provider) return false;
      if (status === "enabled" && !accountIsEnabled(account)) return false;
      if (status === "paused" && accountIsEnabled(account)) return false;
      return true;
    })
    .slice()
    .sort((left, right) => compareAccounts(left, right, sort));
}
