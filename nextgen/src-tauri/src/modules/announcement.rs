use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};

use super::config;
use super::logger;

const ANNOUNCEMENT_READ_IDS_FILE: &str = "announcement_read_ids.json";
const ANNOUNCEMENT_LOCAL_OVERRIDE_FILE: &str = "announcements.local.json";

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnouncementAction {
    #[serde(rename = "type")]
    pub action_type: String,
    pub target: String,
    pub label: String,
    #[serde(default)]
    pub arguments: Option<Vec<serde_json::Value>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnouncementActionOverride {
    #[serde(default = "default_target_versions")]
    pub target_versions: String,
    #[serde(default)]
    pub action: Option<AnnouncementAction>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnouncementLocale {
    #[serde(default)]
    pub title: Option<String>,
    #[serde(default)]
    pub summary: Option<String>,
    #[serde(default)]
    pub content: Option<String>,
    #[serde(default)]
    pub action_label: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnouncementImage {
    pub url: String,
    #[serde(default)]
    pub label: Option<String>,
    #[serde(default)]
    pub alt: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Announcement {
    pub id: String,
    #[serde(rename = "type")]
    pub announcement_type: String,
    #[serde(default)]
    pub priority: i64,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub summary: String,
    #[serde(default)]
    pub content: String,
    #[serde(default)]
    pub action: Option<AnnouncementAction>,
    #[serde(default)]
    pub action_overrides: Option<Vec<AnnouncementActionOverride>>,
    #[serde(default = "default_target_versions")]
    pub target_versions: String,
    #[serde(default)]
    pub target_languages: Option<Vec<String>>,
    #[serde(default)]
    pub show_once: Option<bool>,
    #[serde(default)]
    pub popup: bool,
    #[serde(default)]
    pub created_at: String,
    #[serde(default)]
    pub expires_at: Option<String>,
    #[serde(default)]
    pub locales: Option<HashMap<String, AnnouncementLocale>>,
    #[serde(default)]
    pub images: Option<Vec<AnnouncementImage>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TopRightAdLocale {
    #[serde(default)]
    pub text: Option<String>,
    #[serde(default)]
    pub badge: Option<String>,
    #[serde(default)]
    pub cta_label: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TopRightAd {
    pub id: String,
    #[serde(default = "default_true")]
    pub enabled: bool,
    #[serde(default)]
    pub relay_related: bool,
    #[serde(default)]
    pub priority: i64,
    pub text: String,
    #[serde(default)]
    pub badge: Option<String>,
    #[serde(default)]
    pub cta_label: Option<String>,
    #[serde(default)]
    pub cta_url: Option<String>,
    #[serde(default)]
    pub display_mode: Option<String>,
    #[serde(default)]
    pub display_pages: Option<Vec<String>>,
    #[serde(default)]
    pub display_platforms: Option<Vec<String>>,
    #[serde(default)]
    pub exclude_pages: Option<Vec<String>>,
    #[serde(default)]
    pub exclude_platforms: Option<Vec<String>>,
    #[serde(default = "default_target_versions")]
    pub target_versions: String,
    #[serde(default)]
    pub target_languages: Option<Vec<String>>,
    #[serde(default)]
    pub created_at: String,
    #[serde(default)]
    pub expires_at: Option<String>,
    #[serde(default)]
    pub locales: Option<HashMap<String, TopRightAdLocale>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SponsorLocale {
    #[serde(default)]
    pub badge: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SponsorIntegration {
    #[serde(default)]
    pub enabled: bool,
    #[serde(rename = "type")]
    pub integration_type: String,
    #[serde(default)]
    pub base_url: String,
    #[serde(default)]
    pub wire_api: Option<String>,
    #[serde(default)]
    pub quick_configure: bool,
    #[serde(default)]
    pub dashboard_card: bool,
    #[serde(default)]
    pub models: Vec<String>,
    #[serde(default)]
    pub supports_vision: bool,
    #[serde(default)]
    pub website: Option<String>,
    #[serde(default)]
    pub api_key_url: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Sponsor {
    pub id: String,
    pub name: String,
    #[serde(default)]
    pub priority: i64,
    #[serde(default)]
    pub logo_url: Option<String>,
    #[serde(default)]
    pub url: Option<String>,
    #[serde(default)]
    pub badge: Option<String>,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub integration: Option<SponsorIntegration>,
    #[serde(default = "default_target_versions")]
    pub target_versions: String,
    #[serde(default)]
    pub target_languages: Option<Vec<String>>,
    #[serde(default)]
    pub created_at: String,
    #[serde(default)]
    pub expires_at: Option<String>,
    #[serde(default)]
    pub locales: Option<HashMap<String, SponsorLocale>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SponsorModuleLocale {
    #[serde(default)]
    pub title: Option<String>,
    #[serde(default)]
    pub subtitle: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SponsorModule {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default = "default_true")]
    pub entry_visible: bool,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub subtitle: String,
    #[serde(default = "default_target_versions")]
    pub target_versions: String,
    #[serde(default)]
    pub target_languages: Option<Vec<String>>,
    #[serde(default)]
    pub created_at: String,
    #[serde(default)]
    pub expires_at: Option<String>,
    #[serde(default)]
    pub locales: Option<HashMap<String, SponsorModuleLocale>>,
    #[serde(default)]
    pub sponsors: Vec<Sponsor>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct AnnouncementResponse {
    #[serde(default)]
    pub version: String,
    #[serde(default)]
    pub force_refresh_versions: Vec<String>,
    #[serde(default)]
    pub announcements: Vec<Announcement>,
    #[serde(default)]
    pub top_right_ad: Option<TopRightAd>,
    #[serde(default = "default_true")]
    pub api_relay_enabled: bool,
    #[serde(default = "default_true")]
    pub top_right_ads_enabled: bool,
    #[serde(default)]
    pub top_right_ads: Vec<TopRightAd>,
    #[serde(default)]
    pub sponsor_module: Option<SponsorModule>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnouncementState {
    pub announcements: Vec<Announcement>,
    pub unread_ids: Vec<String>,
    pub popup_announcement: Option<Announcement>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TopRightAdState {
    pub ad: Option<TopRightAd>,
    pub ads: Vec<TopRightAd>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SponsorModuleState {
    pub sponsor_module: Option<SponsorModule>,
}

fn default_target_versions() -> String {
    "*".to_string()
}

fn default_true() -> bool {
    true
}

fn empty_announcement_response() -> AnnouncementResponse {
    AnnouncementResponse {
        version: String::new(),
        force_refresh_versions: Vec::new(),
        announcements: Vec::new(),
        top_right_ad: None,
        api_relay_enabled: false,
        top_right_ads_enabled: false,
        top_right_ads: Vec::new(),
        sponsor_module: None,
    }
}

fn get_shared_dir() -> Result<PathBuf, String> {
    let dir = config::get_shared_dir();
    if !dir.exists() {
        fs::create_dir_all(&dir).map_err(|e| format!("创建公告目录失败: {}", e))?;
    }
    Ok(dir)
}

fn get_read_ids_path() -> Result<PathBuf, String> {
    Ok(get_shared_dir()?.join(ANNOUNCEMENT_READ_IDS_FILE))
}

fn get_local_override_path() -> Result<PathBuf, String> {
    Ok(get_shared_dir()?.join(ANNOUNCEMENT_LOCAL_OVERRIDE_FILE))
}

fn get_workspace_announcement_path() -> Option<PathBuf> {
    if !cfg!(debug_assertions) {
        return None;
    }
    Some(
        PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("..")
            .join("announcements.json"),
    )
}

fn parse_announcement_file(path: &Path) -> Result<AnnouncementResponse, String> {
    let content = fs::read_to_string(path)
        .map_err(|e| format!("读取公告文件失败({}): {}", path.display(), e))?;
    serde_json::from_str::<AnnouncementResponse>(&content)
        .map_err(|e| format!("解析公告文件失败({}): {}", path.display(), e))
}

fn load_local_announcements() -> Result<Option<AnnouncementResponse>, String> {
    if !cfg!(debug_assertions) {
        return Ok(None);
    }

    let local_override = get_local_override_path()?;
    if local_override.exists() {
        logger::log_info("[Announcement] 使用本地覆盖公告文件 announcements.local.json");
        return parse_announcement_file(&local_override).map(Some);
    }

    if let Some(workspace_path) = get_workspace_announcement_path() {
        if workspace_path.exists() {
            logger::log_info("[Announcement] 使用工作区公告文件 announcements.json");
            return parse_announcement_file(&workspace_path).map(Some);
        }
    }

    Ok(None)
}

fn get_read_ids() -> Result<Vec<String>, String> {
    let path = get_read_ids_path()?;
    if !path.exists() {
        return Ok(Vec::new());
    }
    let content = fs::read_to_string(&path).map_err(|e| format!("读取公告已读状态失败: {}", e))?;
    if content.trim().is_empty() {
        return Ok(Vec::new());
    }
    match serde_json::from_str(&content) {
        Ok(ids) => Ok(ids),
        Err(error) => {
            match crate::modules::atomic_write::quarantine_file(&path, "invalid-json") {
                Ok(Some(backup_path)) => logger::log_warn(&format!(
                    "[Announcement] 公告已读状态解析失败，已隔离并使用空状态: path={}, backup={}, error={}",
                    path.display(),
                    backup_path.display(),
                    error
                )),
                Ok(None) => logger::log_warn(&format!(
                    "[Announcement] 公告已读状态解析失败，文件已不存在，使用空状态: path={}, error={}",
                    path.display(),
                    error
                )),
                Err(backup_error) => logger::log_warn(&format!(
                    "[Announcement] 公告已读状态解析失败，隔离失败，使用空状态: path={}, parse_error={}, backup_error={}",
                    path.display(),
                    error,
                    backup_error
                )),
            }
            Ok(Vec::new())
        }
    }
}

fn save_read_ids(ids: &[String]) -> Result<(), String> {
    let content =
        serde_json::to_string_pretty(ids).map_err(|e| format!("序列化公告已读状态失败: {}", e))?;
    crate::modules::atomic_write::write_string_atomic(&get_read_ids_path()?, &content)
        .map_err(|e| format!("写入公告已读状态失败: {}", e))
}

fn parse_version(value: &str) -> Vec<i64> {
    let trimmed = value.trim_start_matches(|c: char| !c.is_ascii_digit());
    trimmed
        .split('.')
        .map(|part| part.parse::<i64>().unwrap_or(0))
        .collect()
}

fn match_version(current_version: &str, pattern: &str) -> bool {
    if pattern.trim().is_empty() || pattern.trim() == "*" {
        return true;
    }

    let (operator, version_str) = if let Some(rest) = pattern.strip_prefix(">=") {
        (">=", rest)
    } else if let Some(rest) = pattern.strip_prefix("<=") {
        ("<=", rest)
    } else if let Some(rest) = pattern.strip_prefix('>') {
        (">", rest)
    } else if let Some(rest) = pattern.strip_prefix('<') {
        ("<", rest)
    } else if let Some(rest) = pattern.strip_prefix('=') {
        ("=", rest)
    } else {
        ("=", pattern)
    };

    let current = parse_version(current_version);
    let target = parse_version(version_str);

    let mut cmp = 0;
    for idx in 0..3 {
        let c = *current.get(idx).unwrap_or(&0);
        let t = *target.get(idx).unwrap_or(&0);
        if c != t {
            cmp = if c > t { 1 } else { -1 };
            break;
        }
    }

    match operator {
        ">=" => cmp >= 0,
        "<=" => cmp <= 0,
        ">" => cmp > 0,
        "<" => cmp < 0,
        _ => cmp == 0,
    }
}

fn is_language_match(current_locale: &str, target_languages: &[String]) -> bool {
    if target_languages.is_empty() || target_languages.iter().any(|lang| lang == "*") {
        return true;
    }

    let current = current_locale.to_lowercase();
    target_languages.iter().any(|lang| {
        let normalized = lang.to_lowercase();
        normalized == current || current.starts_with(&(normalized + "-"))
    })
}

fn parse_datetime_millis(value: &str) -> Option<i64> {
    DateTime::parse_from_rfc3339(value)
        .ok()
        .map(|dt| dt.with_timezone(&Utc).timestamp_millis())
}

fn apply_localized_content(
    announcement: &Announcement,
    locale: &str,
) -> (Announcement, Option<String>) {
    let mut localized = announcement.clone();
    let mut localized_action_label: Option<String> = None;

    if let Some(locales) = &announcement.locales {
        let lower_locale = locale.to_lowercase();
        let matched_key = locales.keys().find(|key| {
            let normalized_key = key.to_lowercase();
            normalized_key == lower_locale || lower_locale.starts_with(&normalized_key)
        });

        if let Some(key) = matched_key {
            if let Some(localized_data) = locales.get(key) {
                if let Some(title) = &localized_data.title {
                    localized.title = title.clone();
                }
                if let Some(summary) = &localized_data.summary {
                    localized.summary = summary.clone();
                }
                if let Some(content) = &localized_data.content {
                    localized.content = content.clone();
                }
                localized_action_label = localized_data.action_label.clone();
            }
        }
    }

    (localized, localized_action_label)
}

fn resolve_action(
    announcement: &Announcement,
    current_version: &str,
) -> Option<AnnouncementAction> {
    let mut action = announcement.action.clone();
    if let Some(overrides) = &announcement.action_overrides {
        for override_item in overrides {
            let pattern = if override_item.target_versions.trim().is_empty() {
                "*"
            } else {
                override_item.target_versions.as_str()
            };
            if match_version(current_version, pattern) {
                action = override_item.action.clone();
                break;
            }
        }
    }
    action
}

fn filter_announcements(
    raw: Vec<Announcement>,
    current_version: &str,
    locale: &str,
) -> Vec<Announcement> {
    let now = Utc::now().timestamp_millis();
    let mut filtered: Vec<Announcement> = raw
        .into_iter()
        .filter_map(|announcement| {
            let target_versions = if announcement.target_versions.trim().is_empty() {
                "*"
            } else {
                announcement.target_versions.as_str()
            };

            if !match_version(current_version, target_versions) {
                return None;
            }

            if let Some(target_languages) = &announcement.target_languages {
                if !is_language_match(locale, target_languages) {
                    return None;
                }
            }

            if let Some(expires_at) = &announcement.expires_at {
                if let Some(expire_ms) = parse_datetime_millis(expires_at) {
                    if expire_ms < now {
                        return None;
                    }
                }
            }

            let (mut localized, localized_action_label) =
                apply_localized_content(&announcement, locale);
            let mut action = resolve_action(&localized, current_version);
            if let (Some(action_item), Some(action_label)) = (&mut action, localized_action_label) {
                action_item.label = action_label;
            }
            localized.action = action;

            Some(localized)
        })
        .collect();

    filtered.sort_by(|a, b| {
        let a_time = parse_datetime_millis(&a.created_at).unwrap_or(0);
        let b_time = parse_datetime_millis(&b.created_at).unwrap_or(0);
        b_time.cmp(&a_time).then(b.priority.cmp(&a.priority))
    });
    filtered
}

fn apply_localized_top_right_ad(ad: &TopRightAd, locale: &str) -> TopRightAd {
    let mut localized = ad.clone();
    if let Some(locales) = &ad.locales {
        let lower_locale = locale.to_lowercase();
        let matched_key = locales.keys().find(|key| {
            let normalized_key = key.to_lowercase();
            normalized_key == lower_locale || lower_locale.starts_with(&(normalized_key + "-"))
        });

        if let Some(key) = matched_key {
            if let Some(localized_data) = locales.get(key) {
                if let Some(text) = &localized_data.text {
                    localized.text = text.clone();
                }
                if let Some(badge) = &localized_data.badge {
                    localized.badge = Some(badge.clone());
                }
                if let Some(cta_label) = &localized_data.cta_label {
                    localized.cta_label = Some(cta_label.clone());
                }
            }
        }
    }

    localized
}

fn filter_top_right_ad_item(
    mut item: TopRightAd,
    current_version: &str,
    locale: &str,
    api_relay_enabled: bool,
) -> Option<TopRightAd> {
    if !item.enabled {
        return None;
    }
    if item.relay_related && !api_relay_enabled {
        return None;
    }

    let target_versions = if item.target_versions.trim().is_empty() {
        "*"
    } else {
        item.target_versions.as_str()
    };
    if !match_version(current_version, target_versions) {
        return None;
    }
    if let Some(target_languages) = &item.target_languages {
        if !is_language_match(locale, target_languages) {
            return None;
        }
    }

    if let Some(expires_at) = &item.expires_at {
        if let Some(expire_ms) = parse_datetime_millis(expires_at) {
            if expire_ms < Utc::now().timestamp_millis() {
                return None;
            }
        }
    }

    item = apply_localized_top_right_ad(&item, locale);
    Some(item)
}

fn filter_top_right_ad(
    ad: Option<TopRightAd>,
    current_version: &str,
    locale: &str,
    api_relay_enabled: bool,
) -> Option<TopRightAd> {
    filter_top_right_ad_item(ad?, current_version, locale, api_relay_enabled)
}

fn filter_top_right_ads(
    ads: Vec<TopRightAd>,
    current_version: &str,
    locale: &str,
    api_relay_enabled: bool,
) -> Vec<TopRightAd> {
    let mut filtered: Vec<TopRightAd> = ads
        .into_iter()
        .filter_map(|item| {
            filter_top_right_ad_item(item, current_version, locale, api_relay_enabled)
        })
        .collect();

    filtered.sort_by(|a, b| {
        let a_time = parse_datetime_millis(&a.created_at).unwrap_or(0);
        let b_time = parse_datetime_millis(&b.created_at).unwrap_or(0);
        b.priority.cmp(&a.priority).then(b_time.cmp(&a_time))
    });

    filtered
}

fn apply_localized_sponsor_module(module: &SponsorModule, locale: &str) -> SponsorModule {
    let mut localized = module.clone();
    if let Some(locales) = &module.locales {
        let lower_locale = locale.to_lowercase();
        let matched_key = locales.keys().find(|key| {
            let normalized_key = key.to_lowercase();
            normalized_key == lower_locale || lower_locale.starts_with(&(normalized_key + "-"))
        });

        if let Some(key) = matched_key {
            if let Some(localized_data) = locales.get(key) {
                if let Some(title) = &localized_data.title {
                    localized.title = title.clone();
                }
                if let Some(subtitle) = &localized_data.subtitle {
                    localized.subtitle = subtitle.clone();
                }
            }
        }
    }

    localized
}

fn apply_localized_sponsor(sponsor: &Sponsor, locale: &str) -> Sponsor {
    let mut localized = sponsor.clone();
    if let Some(locales) = &sponsor.locales {
        let lower_locale = locale.to_lowercase();
        let matched_key = locales.keys().find(|key| {
            let normalized_key = key.to_lowercase();
            normalized_key == lower_locale || lower_locale.starts_with(&(normalized_key + "-"))
        });

        if let Some(key) = matched_key {
            if let Some(localized_data) = locales.get(key) {
                if let Some(badge) = &localized_data.badge {
                    localized.badge = Some(badge.clone());
                }
                if let Some(description) = &localized_data.description {
                    localized.description = description.clone();
                }
            }
        }
    }

    localized
}

fn is_visible_for_current_context(
    target_versions: &str,
    target_languages: &Option<Vec<String>>,
    expires_at: &Option<String>,
    current_version: &str,
    locale: &str,
) -> bool {
    let target_versions = if target_versions.trim().is_empty() {
        "*"
    } else {
        target_versions
    };
    if !match_version(current_version, target_versions) {
        return false;
    }
    if let Some(languages) = target_languages {
        if !is_language_match(locale, languages) {
            return false;
        }
    }
    if let Some(expires_at) = expires_at {
        if let Some(expire_ms) = parse_datetime_millis(expires_at) {
            if expire_ms < Utc::now().timestamp_millis() {
                return false;
            }
        }
    }
    true
}

fn filter_sponsor_module(
    module: Option<SponsorModule>,
    current_version: &str,
    locale: &str,
) -> Option<SponsorModule> {
    let mut module = module?;
    if !module.enabled || !module.entry_visible {
        return None;
    }
    if !is_visible_for_current_context(
        &module.target_versions,
        &module.target_languages,
        &module.expires_at,
        current_version,
        locale,
    ) {
        return None;
    }

    module = apply_localized_sponsor_module(&module, locale);
    module.sponsors = module
        .sponsors
        .into_iter()
        .filter(|sponsor| {
            !sponsor.id.trim().is_empty()
                && !sponsor.name.trim().is_empty()
                && is_visible_for_current_context(
                    &sponsor.target_versions,
                    &sponsor.target_languages,
                    &sponsor.expires_at,
                    current_version,
                    locale,
                )
        })
        .map(|sponsor| apply_localized_sponsor(&sponsor, locale))
        .collect();

    module.sponsors.sort_by(|a, b| {
        let a_time = parse_datetime_millis(&a.created_at).unwrap_or(0);
        let b_time = parse_datetime_millis(&b.created_at).unwrap_or(0);
        b.priority.cmp(&a.priority).then(b_time.cmp(&a_time))
    });

    Some(module)
}

async fn load_announcements_raw() -> Result<AnnouncementResponse, String> {
    if let Some(local_data) = load_local_announcements()? {
        return Ok(local_data);
    }
    Ok(empty_announcement_response())
}

pub async fn get_announcement_state() -> Result<AnnouncementState, String> {
    let current_version = env!("CARGO_PKG_VERSION");
    let locale = config::get_user_config().language.to_lowercase();
    let raw_payload = load_announcements_raw().await?;
    let announcements = filter_announcements(raw_payload.announcements, current_version, &locale);
    let read_ids = get_read_ids()?;

    let unread_ids: Vec<String> = announcements
        .iter()
        .filter(|item| !read_ids.contains(&item.id))
        .map(|item| item.id.clone())
        .collect();

    let popup_announcement = announcements
        .iter()
        .find(|item| item.popup && !read_ids.contains(&item.id))
        .cloned();

    Ok(AnnouncementState {
        announcements,
        unread_ids,
        popup_announcement,
    })
}

pub async fn get_top_right_ad_state() -> Result<TopRightAdState, String> {
    let current_version = env!("CARGO_PKG_VERSION");
    let locale = config::get_user_config().language.to_lowercase();
    let raw_payload = load_announcements_raw().await?;
    if !raw_payload.top_right_ads_enabled {
        return Ok(TopRightAdState {
            ad: None,
            ads: Vec::new(),
        });
    }

    let ad = filter_top_right_ad(
        raw_payload.top_right_ad,
        current_version,
        &locale,
        raw_payload.api_relay_enabled,
    );
    let ads = filter_top_right_ads(
        raw_payload.top_right_ads,
        current_version,
        &locale,
        raw_payload.api_relay_enabled,
    );
    Ok(TopRightAdState { ad, ads })
}

pub async fn get_sponsor_module_state() -> Result<SponsorModuleState, String> {
    let current_version = env!("CARGO_PKG_VERSION");
    let locale = config::get_user_config().language.to_lowercase();
    let raw_payload = load_announcements_raw().await?;
    if !raw_payload.api_relay_enabled {
        return Ok(SponsorModuleState {
            sponsor_module: None,
        });
    }
    let sponsor_module =
        filter_sponsor_module(raw_payload.sponsor_module, current_version, &locale);
    Ok(SponsorModuleState { sponsor_module })
}

pub async fn force_refresh_sponsor_module() -> Result<SponsorModuleState, String> {
    get_sponsor_module_state().await
}

pub async fn force_refresh_top_right_ad() -> Result<TopRightAdState, String> {
    get_top_right_ad_state().await
}

pub async fn mark_announcement_as_read(id: &str) -> Result<(), String> {
    let mut read_ids = get_read_ids()?;
    if !read_ids.iter().any(|item| item == id) {
        read_ids.push(id.to_string());
        save_read_ids(&read_ids)?;
    }
    Ok(())
}

pub async fn mark_all_announcements_as_read() -> Result<(), String> {
    let current_version = env!("CARGO_PKG_VERSION");
    let locale = config::get_user_config().language.to_lowercase();
    let raw_payload = load_announcements_raw().await?;
    let announcements = filter_announcements(raw_payload.announcements, current_version, &locale);
    let ids: Vec<String> = announcements.iter().map(|item| item.id.clone()).collect();
    save_read_ids(&ids)
}

pub async fn force_refresh_announcements() -> Result<AnnouncementState, String> {
    get_announcement_state().await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_payload_disables_announcements_ads_and_sponsors() {
        let payload = empty_announcement_response();
        assert!(payload.announcements.is_empty());
        assert!(payload.top_right_ad.is_none());
        assert!(payload.top_right_ads.is_empty());
        assert!(!payload.api_relay_enabled);
        assert!(!payload.top_right_ads_enabled);
        assert!(payload.sponsor_module.is_none());
    }
}
