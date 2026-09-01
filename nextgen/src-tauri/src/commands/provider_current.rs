use tauri::AppHandle;

fn resolve_provider_current_account_id(platform: &str) -> Result<Option<String>, String> {
    match platform {
        "windsurf" => {
            let accounts = crate::modules::windsurf_account::list_accounts();
            Ok(crate::modules::windsurf_account::resolve_current_account_id(&accounts))
        }
        "cursor" => {
            let accounts = crate::modules::cursor_account::list_accounts();
            Ok(crate::modules::cursor_account::resolve_current_account_id(
                &accounts,
            ))
        }
        other => Err(format!("不支持的平台: {}", other)),
    }
}

#[tauri::command]
pub async fn get_provider_current_account_id(
    app: AppHandle,
    platform: String,
) -> Result<Option<String>, String> {
    let current_account_id = resolve_provider_current_account_id(platform.trim())?;
    let _ = crate::modules::tray::update_tray_menu(&app);
    Ok(current_account_id)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::path::PathBuf;

    struct DataDirGuard {
        dir: PathBuf,
        previous_data_dir: Option<String>,
    }

    impl DataDirGuard {
        fn new(name: &str) -> Self {
            let dir = std::env::temp_dir().join(format!(
                "cockpit-provider-current-command-{}-{}",
                name,
                std::process::id()
            ));
            let _ = fs::remove_dir_all(&dir);
            fs::create_dir_all(&dir).expect("create temp data dir");
            let previous_data_dir = std::env::var("XIASS_TOOLS_DATA_DIR").ok();
            std::env::set_var("XIASS_TOOLS_DATA_DIR", &dir);
            Self {
                dir,
                previous_data_dir,
            }
        }
    }

    impl Drop for DataDirGuard {
        fn drop(&mut self) {
            match self.previous_data_dir.as_ref() {
                Some(value) => std::env::set_var("XIASS_TOOLS_DATA_DIR", value),
                None => std::env::remove_var("XIASS_TOOLS_DATA_DIR"),
            }
            let _ = fs::remove_dir_all(&self.dir);
        }
    }

    #[test]
    fn provider_current_command_supports_production_generic_provider_pages() {
        let _lock = crate::modules::test_support::lock_env();
        let _guard = DataDirGuard::new("supported-platforms");

        for platform in ["windsurf", "cursor"] {
            let result = resolve_provider_current_account_id(platform)
                .unwrap_or_else(|err| panic!("platform {platform} should be supported: {err}"));
            assert_eq!(
                result, None,
                "empty data dir should have no current account"
            );
        }
    }

    #[test]
    fn provider_current_command_rejects_hidden_platforms() {
        for platform in ["kiro", "codebuddy", "trae", "github-copilot", "zed"] {
            assert!(resolve_provider_current_account_id(platform).is_err());
        }
    }

    #[test]
    fn provider_current_command_rejects_unknown_platform() {
        let _lock = crate::modules::test_support::lock_env();
        let _guard = DataDirGuard::new("unsupported-platform");

        let error = resolve_provider_current_account_id("unknown-platform")
            .expect_err("unknown platform should be rejected");
        assert!(error.contains("不支持的平台"));
    }
}
