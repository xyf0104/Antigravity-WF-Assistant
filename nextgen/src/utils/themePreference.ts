export type ThemePreference = 'light' | 'dark' | 'system';
export type AppliedTheme = Exclude<ThemePreference, 'system'>;

export const THEME_PREFERENCE_STORAGE_KEY = 'xiass.tools.theme-preference';
export const THEME_PREFERENCE_INTENT_EVENT = 'xiass:theme-preference-intent';

export const isThemePreference = (value: unknown): value is ThemePreference =>
  value === 'light' || value === 'dark' || value === 'system';

export const normalizeThemePreference = (value: unknown): ThemePreference =>
  isThemePreference(value) ? value : 'system';

/**
 * Resolve the persisted choice to the concrete palette that is rendered in
 * this WebView. Keeping this in one place prevents the shell and an embedded
 * workspace from briefly resolving system mode differently.
 */
export const resolveAppliedTheme = (preference: unknown): AppliedTheme => {
  const normalizedPreference = normalizeThemePreference(preference);
  if (normalizedPreference !== 'system') {
    return normalizedPreference;
  }

  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
};

/**
 * Keep both document roots in sync. Some embedded UI and native WebView
 * surfaces inherit from body while the main application inherits from html.
 */
export const applyDocumentTheme = (preference: unknown): AppliedTheme => {
  const appliedTheme = resolveAppliedTheme(preference);
  document.documentElement.setAttribute('data-theme', appliedTheme);
  document.body?.setAttribute('data-theme', appliedTheme);
  return appliedTheme;
};

/**
 * Reads the last explicit theme choice made in this WebView. This is UI-only
 * state: it contains no account or configuration data and preserves the last
 * user-selected appearance while the native configuration catches up.
 */
export const readPersistedThemePreference = (): ThemePreference | null => {
  try {
    const preference = window.localStorage.getItem(THEME_PREFERENCE_STORAGE_KEY);
    return isThemePreference(preference) ? preference : null;
  } catch {
    return null;
  }
};

/**
 * The browser-side value is written at the point where the user selects a
 * theme, while the native configuration can still be returning an older value
 * during startup or after a quick page change. Prefer that user-facing value
 * whenever it exists; only a fresh installation without the value falls back
 * to native configuration.
 */
export const resolveThemePreference = (nativePreference: unknown): ThemePreference => {
  const normalizedNativePreference = normalizeThemePreference(nativePreference);
  const persistedPreference = readPersistedThemePreference();
  return persistedPreference ?? normalizedNativePreference;
};

export const persistThemePreference = (preference: ThemePreference) => {
  try {
    window.localStorage.setItem(THEME_PREFERENCE_STORAGE_KEY, preference);
  } catch {
    // The native configuration remains authoritative when storage is unavailable.
  }
};

export const dispatchThemePreferenceIntent = (preference: ThemePreference) => {
  window.dispatchEvent(
    new CustomEvent<{ theme: ThemePreference }>(THEME_PREFERENCE_INTENT_EVENT, {
      detail: { theme: preference },
    }),
  );
};
