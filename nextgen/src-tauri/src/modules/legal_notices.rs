//! Offline legal notice documents bundled with XIASS Tools.
//!
//! The desktop command intentionally has no filename or path argument.  The
//! document list is fixed here so the UI can only read the notices shipped with
//! the application and can never turn this into a local-file reader.

use serde::Serialize;
use std::fs;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use tauri::{AppHandle, Manager};

const BUNDLED_LICENSES_DIR: &str = "licenses";
const LEGAL_NOTICE_LOAD_ERROR: &str = "无法读取内置许可资料，请重新安装 XIASS Tools 后重试。";

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LegalNoticeDocument {
    /// Stable, UI-facing identifier. This is not a file name or path.
    pub id: &'static str,
    pub title: &'static str,
    pub content: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LegalNoticeCollection {
    pub notices: Vec<LegalNoticeDocument>,
}

#[derive(Debug, Clone, Copy)]
struct LegalNoticeSpec {
    id: &'static str,
    title: &'static str,
    bundled_file_name: &'static str,
    development_file_name: &'static str,
}

// Keep this allowlist deliberately small and explicit. The bundle manifest in
// tauri.conf.json maps these exact source documents into `licenses/`.
const LEGAL_NOTICE_SPECS: [LegalNoticeSpec; 4] = [
    LegalNoticeSpec {
        id: "origin_and_license",
        title: "来源与许可说明",
        bundled_file_name: "ORIGIN_AND_LICENSE.md",
        development_file_name: "ORIGIN_AND_LICENSE.md",
    },
    LegalNoticeSpec {
        id: "third_party_notices",
        title: "第三方声明",
        bundled_file_name: "THIRD_PARTY_NOTICES.md",
        development_file_name: "THIRD_PARTY_NOTICES.md",
    },
    LegalNoticeSpec {
        id: "cc_by_nc_sa_4_0",
        title: "CC BY-NC-SA 4.0 法律文本",
        bundled_file_name: "CC-BY-NC-SA-4.0-LEGALCODE.txt",
        development_file_name: "CC-BY-NC-SA-4.0-LEGALCODE.txt",
    },
    LegalNoticeSpec {
        id: "xiass_nextgen_license",
        title: "XIASS Tools Nextgen 许可",
        bundled_file_name: "XIASS-Tools-Nextgen-CC-BY-NC-SA-4.0.txt",
        development_file_name: "LICENSE",
    },
];

/// Loads the fixed legal-notice catalog from the installed application
/// resources. Debug builds may use the compile-time source tree as a fallback
/// when Tauri has not copied resources yet.
pub fn load_bundled_legal_notices(app: &AppHandle) -> Result<LegalNoticeCollection, String> {
    let bundled_resource_dir = app.path().resource_dir().ok();
    let development_root = development_fallback_root();

    load_legal_notices_from_roots(bundled_resource_dir.as_deref(), development_root.as_deref())
}

fn development_fallback_root() -> Option<PathBuf> {
    if !cfg!(debug_assertions) {
        return None;
    }

    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .map(Path::to_path_buf)
}

fn load_legal_notices_from_roots(
    bundled_resource_dir: Option<&Path>,
    development_root: Option<&Path>,
) -> Result<LegalNoticeCollection, String> {
    let notices = LEGAL_NOTICE_SPECS
        .iter()
        .map(|spec| {
            read_notice_content(*spec, bundled_resource_dir, development_root).map(|content| {
                LegalNoticeDocument {
                    id: spec.id,
                    title: spec.title,
                    content,
                }
            })
        })
        .collect::<Result<Vec<_>, _>>()?;

    Ok(LegalNoticeCollection { notices })
}

fn read_notice_content(
    spec: LegalNoticeSpec,
    bundled_resource_dir: Option<&Path>,
    development_root: Option<&Path>,
) -> Result<String, String> {
    if let Some(resource_dir) = bundled_resource_dir {
        let bundled_path = resource_dir
            .join(BUNDLED_LICENSES_DIR)
            .join(spec.bundled_file_name);
        match fs::read_to_string(bundled_path) {
            Ok(content) => return Ok(content),
            // A missing development resource is expected before `cargo tauri`
            // prepares its resource directory. Other errors must not silently
            // fall back to a different copy of the document.
            Err(error) if error.kind() == ErrorKind::NotFound => {}
            Err(_) => return Err(LEGAL_NOTICE_LOAD_ERROR.to_string()),
        }
    }

    if let Some(root) = development_root {
        return fs::read_to_string(root.join(spec.development_file_name))
            .map_err(|_| LEGAL_NOTICE_LOAD_ERROR.to_string());
    }

    Err(LEGAL_NOTICE_LOAD_ERROR.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    struct TestDir(PathBuf);

    impl TestDir {
        fn new(label: &str) -> Self {
            let path = std::env::temp_dir().join(format!(
                "xiass-tools-legal-notices-{label}-{}",
                uuid::Uuid::new_v4()
            ));
            fs::create_dir_all(&path).expect("create test directory");
            Self(path)
        }

        fn path(&self) -> &Path {
            &self.0
        }
    }

    impl Drop for TestDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    fn write_bundled_documents(root: &Path, prefix: &str) {
        let licenses = root.join(BUNDLED_LICENSES_DIR);
        fs::create_dir_all(&licenses).expect("create bundled licenses directory");
        for spec in LEGAL_NOTICE_SPECS {
            fs::write(
                licenses.join(spec.bundled_file_name),
                format!("{prefix}:{}", spec.id),
            )
            .expect("write bundled notice");
        }
    }

    fn write_development_documents(root: &Path, prefix: &str) {
        for spec in LEGAL_NOTICE_SPECS {
            fs::write(
                root.join(spec.development_file_name),
                format!("{prefix}:{}", spec.id),
            )
            .expect("write development notice");
        }
    }

    #[test]
    fn catalog_is_a_fixed_path_free_allowlist() {
        let ids = LEGAL_NOTICE_SPECS
            .iter()
            .map(|spec| spec.id)
            .collect::<Vec<_>>();
        assert_eq!(
            ids,
            vec![
                "origin_and_license",
                "third_party_notices",
                "cc_by_nc_sa_4_0",
                "xiass_nextgen_license",
            ]
        );

        for spec in LEGAL_NOTICE_SPECS {
            assert!(!spec.bundled_file_name.contains(['/', '\\']));
            assert!(!spec.development_file_name.contains(['/', '\\']));
        }
    }

    #[test]
    fn reads_all_documents_from_installed_resources_before_development_fallback() {
        let bundled = TestDir::new("bundled");
        let development = TestDir::new("development");
        write_bundled_documents(bundled.path(), "bundled");
        write_development_documents(development.path(), "development");

        let notices = load_legal_notices_from_roots(Some(bundled.path()), Some(development.path()))
            .expect("load bundled legal notices");

        assert_eq!(notices.notices.len(), LEGAL_NOTICE_SPECS.len());
        for notice in notices.notices {
            assert!(notice.content.starts_with("bundled:"));
        }
    }

    #[test]
    fn development_fallback_loads_only_known_source_documents_when_resources_are_missing() {
        let missing_bundle = TestDir::new("missing-bundle");
        let development = TestDir::new("development-fallback");
        write_development_documents(development.path(), "development");

        let notices =
            load_legal_notices_from_roots(Some(missing_bundle.path()), Some(development.path()))
                .expect("load development fallback legal notices");

        assert_eq!(notices.notices.len(), LEGAL_NOTICE_SPECS.len());
        for notice in notices.notices {
            assert!(notice.content.starts_with("development:"));
        }
    }

    #[test]
    fn missing_notices_return_a_safe_error_without_disclosing_local_paths() {
        let missing_bundle = TestDir::new("missing-all");
        let error = load_legal_notices_from_roots(Some(missing_bundle.path()), None)
            .expect_err("missing resources should fail");

        assert_eq!(error, LEGAL_NOTICE_LOAD_ERROR);
        assert!(!error.contains(&missing_bundle.path().display().to_string()));
        assert!(!error.contains(BUNDLED_LICENSES_DIR));
    }
}
