export function normalizeCodexModelProviderCatalog(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const seen = new Set<string>();
  const models: string[] = [];
  for (const item of value) {
    const model = String(item ?? "").trim();
    const key = model.toLowerCase();
    if (!model || seen.has(key)) continue;
    seen.add(key);
    models.push(model);
  }
  return models;
}

// Persist the catalog spelling so a model selected before a refresh remains
// stable even when the upstream changes only its letter casing.
export function normalizeCodexModelProviderCatalogSelection(
  value: unknown,
  catalog: readonly string[],
): string | undefined {
  const selected = String(value ?? "").trim();
  if (!selected) return undefined;
  const matched = catalog.find((model) => model.toLowerCase() === selected.toLowerCase());
  return matched;
}

/**
 * Keep the upstream order while retaining any manually entered selected model.
 * That makes refreshes safe: users can still save a model whose provider does
 * not include it in `/models`, but duplicate IDs never create duplicate rows.
 */
export function mergeCodexModelProviderCatalogOptions(
  ...catalogs: Array<readonly string[] | null | undefined>
): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const catalog of catalogs) {
    for (const entry of catalog ?? []) {
      const model = entry.trim();
      const key = model.toLowerCase();
      if (!model || seen.has(key)) continue;
      seen.add(key);
      result.push(model);
    }
  }
  return result;
}
