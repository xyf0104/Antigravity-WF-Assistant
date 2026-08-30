use rusqlite::Connection;
use std::path::PathBuf;

fn webkit_data_roots() -> Vec<PathBuf> {
    #[cfg(target_os = "macos")]
    {
        let Some(home) = dirs::home_dir() else {
            return Vec::new();
        };
        return [
            "com.xiass.tools",
            "com.jlcodes.xiass-tools",
            "com.jlcodes.cockpit-tools",
        ]
        .into_iter()
        .map(|identifier| {
            home.join("Library/WebKit")
                .join(identifier)
                .join("WebsiteData")
        })
        .collect();
    }
    #[cfg(not(target_os = "macos"))]
    {
        Vec::new()
    }
}

fn find_localstorage_dbs(root: &std::path::Path) -> Vec<PathBuf> {
    let mut results = Vec::new();
    let Ok(entries) = std::fs::read_dir(root) else {
        return results;
    };
    for entry in entries.flatten() {
        let path = entry.path();
        if path.is_dir() {
            let candidate = path.join("LocalStorage").join("localstorage.sqlite3");
            if candidate.exists() {
                results.push(candidate);
            }
            results.extend(find_localstorage_dbs(&path));
        }
    }
    results
}

/// Checkpoint WAL on all WebKit LocalStorage SQLite databases.
///
/// WebKit WKWebView uses WAL mode for LocalStorage but does not
/// periodically checkpoint. When the app writes large blobs frequently
/// (e.g. account caches with quota snapshots), the WAL file grows
/// unbounded — in production it has been observed at 13 GB for only
/// ~5 MB of actual data.
///
/// Running this at startup keeps the WAL from accumulating over time.
pub fn checkpoint_webkit_localstorage() {
    let dbs: Vec<PathBuf> = webkit_data_roots()
        .into_iter()
        .filter(|root| root.exists())
        .flat_map(|root| find_localstorage_dbs(&root))
        .collect();
    if dbs.is_empty() {
        return;
    }

    for db_path in dbs {
        let label = db_path.display();
        match Connection::open(&db_path) {
            Ok(conn) => {
                match conn.execute_batch("PRAGMA wal_checkpoint(TRUNCATE);") {
                    Ok(()) => {
                        crate::modules::logger::log_info(&format!(
                            "[WebkitCache] WAL checkpoint 成功: {}",
                            label
                        ));
                    }
                    Err(e) => {
                        crate::modules::logger::log_warn(&format!(
                            "[WebkitCache] WAL checkpoint 失败 (可能 WebView 正在占用): {} — {}",
                            label, e
                        ));
                    }
                }
                drop(conn);
            }
            Err(e) => {
                crate::modules::logger::log_warn(&format!(
                    "[WebkitCache] 无法打开数据库: {} — {}",
                    label, e
                ));
            }
        }
    }
}

#[cfg(all(test, target_os = "macos"))]
mod tests {
    use super::*;

    #[test]
    fn maintenance_includes_current_and_legacy_bundle_identifiers() {
        let roots = webkit_data_roots();
        let joined = roots
            .iter()
            .map(|path| path.to_string_lossy())
            .collect::<Vec<_>>()
            .join("\n");
        assert!(joined.contains("com.xiass.tools"));
        assert!(joined.contains("com.jlcodes.xiass-tools"));
        assert!(joined.contains("com.jlcodes.cockpit-tools"));
    }
}
