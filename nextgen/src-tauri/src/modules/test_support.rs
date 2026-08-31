use std::ffi::{OsStr, OsString};
use std::sync::{Mutex, MutexGuard, OnceLock};

pub fn env_lock() -> &'static Mutex<()> {
    static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
    LOCK.get_or_init(|| Mutex::new(()))
}

/// Serializes tests that temporarily change process-wide environment variables.
/// Recovering a poisoned lock prevents one assertion failure from turning every
/// later environment-isolated test into an unrelated `PoisonError` failure.
pub fn lock_env() -> MutexGuard<'static, ()> {
    env_lock().lock().unwrap_or_else(|error| error.into_inner())
}

/// Restores one process-wide environment variable exactly as it was when the
/// guard was created, including non-Unicode values and panic unwinding paths.
pub struct ScopedEnvVar {
    key: &'static str,
    previous: Option<OsString>,
}

impl ScopedEnvVar {
    pub fn set(key: &'static str, value: impl AsRef<OsStr>) -> Self {
        let previous = std::env::var_os(key);
        std::env::set_var(key, value);
        Self { key, previous }
    }
}

impl Drop for ScopedEnvVar {
    fn drop(&mut self) {
        match self.previous.as_ref() {
            Some(value) => std::env::set_var(self.key, value),
            None => std::env::remove_var(self.key),
        }
    }
}
