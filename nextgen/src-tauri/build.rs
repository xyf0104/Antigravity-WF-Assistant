#[cfg(target_os = "macos")]
use swift_rs::SwiftLinker;

use std::path::{Path, PathBuf};
use std::process::Command;

#[cfg(target_os = "macos")]
fn link_macos_swift_runtime_rpaths() {
    println!("cargo:rustc-link-arg=-Wl,-rpath,/usr/lib/swift");
}

fn go_target_from_rust_target(target: &str) -> Option<(&'static str, &'static str)> {
    let goos = if target.contains("windows") {
        "windows"
    } else if target.contains("apple-darwin") {
        "darwin"
    } else if target.contains("linux") {
        "linux"
    } else {
        return None;
    };

    let goarch = if target.starts_with("x86_64") {
        "amd64"
    } else if target.starts_with("aarch64") {
        "arm64"
    } else if target.starts_with("i686") {
        "386"
    } else if target.starts_with("armv7") {
        "arm"
    } else {
        return None;
    };

    Some((goos, goarch))
}

fn should_skip_sidecar_build(output: &Path) -> bool {
    let skip = std::env::var("XIASS_SKIP_CLIPROXY_BUILD")
        .or_else(|_| std::env::var("COCKPIT_SKIP_CLIPROXY_BUILD"))
        .ok();
    skip.as_deref() == Some("1") && output.exists()
}

fn emit_sidecar_rerun_inputs(path: &Path) {
    if path.file_name().and_then(|name| name.to_str()) == Some("bin") {
        return;
    }

    let Ok(metadata) = std::fs::metadata(path) else {
        return;
    };

    if metadata.is_dir() {
        let Ok(entries) = std::fs::read_dir(path) else {
            return;
        };
        for entry in entries.flatten() {
            emit_sidecar_rerun_inputs(&entry.path());
        }
        return;
    }

    let should_track = matches!(
        path.file_name().and_then(|name| name.to_str()),
        Some("go.mod") | Some("go.sum")
    ) || path.extension().and_then(|extension| extension.to_str()) == Some("go");

    if should_track {
        println!("cargo:rerun-if-changed={}", path.display());
    }
}

fn emit_wf_bridge_rerun_inputs(path: &Path, source_root: &Path) {
    let Ok(metadata) = std::fs::metadata(path) else {
        return;
    };

    if metadata.is_dir() {
        let Ok(entries) = std::fs::read_dir(path) else {
            return;
        };
        for entry in entries.flatten() {
            emit_wf_bridge_rerun_inputs(&entry.path(), source_root);
        }
        return;
    }

    let is_go_input = matches!(
        path.file_name().and_then(|name| name.to_str()),
        Some("go.mod") | Some("go.sum")
    ) || path.extension().and_then(|extension| extension.to_str()) == Some("go");
    let is_embedded_asset = path.starts_with(source_root.join("frontend/dist"));
    if is_go_input || is_embedded_asset {
        println!("cargo:rerun-if-changed={}", path.display());
    }
}

fn build_go_sidecar(
    sidecar_dir: &Path,
    output_dir: &Path,
    rust_target: &str,
    goos: &str,
    goarch: &str,
) -> PathBuf {
    let extension = if goos == "windows" { ".exe" } else { "" };
    let output = output_dir.join(format!("xiass-cliproxy-{rust_target}{extension}"));
    if should_skip_sidecar_build(&output) {
        return output;
    }

    let status = Command::new("go")
        .current_dir(sidecar_dir)
        .env("GOOS", goos)
        .env("GOARCH", goarch)
        .env("CGO_ENABLED", "0")
        .arg("build")
        .arg("-trimpath")
        .arg("-ldflags")
        .arg("-s -w")
        .arg("-o")
        .arg(&output)
        .arg(".")
        .status()
        .expect("failed to start go build for xiass-cliproxy");

    if !status.success() {
        panic!("go build for xiass-cliproxy failed with status: {status}");
    }

    output
}

fn build_macos_universal_sidecar(sidecar_dir: &Path, output_dir: &Path) {
    let output = output_dir.join("xiass-cliproxy-universal-apple-darwin");
    if should_skip_sidecar_build(&output) {
        return;
    }

    let x86_64_output = build_go_sidecar(
        sidecar_dir,
        output_dir,
        "x86_64-apple-darwin",
        "darwin",
        "amd64",
    );
    let aarch64_output = build_go_sidecar(
        sidecar_dir,
        output_dir,
        "aarch64-apple-darwin",
        "darwin",
        "arm64",
    );

    let status = Command::new("lipo")
        .arg("-create")
        .arg(&x86_64_output)
        .arg(&aarch64_output)
        .arg("-output")
        .arg(&output)
        .status()
        .expect("failed to start lipo for xiass-cliproxy universal sidecar");

    if !status.success() {
        panic!("lipo for xiass-cliproxy universal sidecar failed with status: {status}");
    }
}

fn should_skip_wf_bridge_build(output: &Path) -> bool {
    let skip = std::env::var("XIASS_SKIP_WF_BRIDGE_BUILD")
        .or_else(|_| std::env::var("COCKPIT_SKIP_WF_BRIDGE_BUILD"))
        .ok();
    skip.as_deref() == Some("1") && output.exists()
}

fn build_go_wf_bridge(
    source_dir: &Path,
    output_dir: &Path,
    rust_target: &str,
    goos: &str,
    goarch: &str,
) -> PathBuf {
    let extension = if goos == "windows" { ".exe" } else { "" };
    let output = output_dir.join(format!("xiass-wf-bridge-{rust_target}{extension}"));
    if should_skip_wf_bridge_build(&output) {
        return output;
    }

    let status = Command::new("go")
        .current_dir(source_dir)
        .env("GOOS", goos)
        .env("GOARCH", goarch)
        .env("CGO_ENABLED", "0")
        .arg("build")
        .arg("-tags")
        .arg("wfbridge")
        .arg("-trimpath")
        .arg("-ldflags")
        .arg("-s -w")
        .arg("-o")
        .arg(&output)
        .arg(".")
        .status()
        .expect("failed to start go build for xiass-wf-bridge");

    if !status.success() {
        panic!("go build for xiass-wf-bridge failed with status: {status}");
    }
    output
}

fn build_macos_universal_wf_bridge(source_dir: &Path, output_dir: &Path) {
    let output = output_dir.join("xiass-wf-bridge-universal-apple-darwin");
    if should_skip_wf_bridge_build(&output) {
        return;
    }

    let x86_64_output = build_go_wf_bridge(
        source_dir,
        output_dir,
        "x86_64-apple-darwin",
        "darwin",
        "amd64",
    );
    let aarch64_output = build_go_wf_bridge(
        source_dir,
        output_dir,
        "aarch64-apple-darwin",
        "darwin",
        "arm64",
    );
    let status = Command::new("lipo")
        .arg("-create")
        .arg(&x86_64_output)
        .arg(&aarch64_output)
        .arg("-output")
        .arg(&output)
        .status()
        .expect("failed to start lipo for xiass-wf-bridge universal sidecar");
    if !status.success() {
        panic!("lipo for xiass-wf-bridge universal sidecar failed with status: {status}");
    }
}

fn build_xiass_cliproxy_sidecar() {
    let manifest_dir =
        PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR is required"));
    let target = std::env::var("TARGET").expect("TARGET is required");
    println!("cargo:rustc-env=XIASS_RUST_TARGET={target}");
    let sidecar_dir = manifest_dir.join("../sidecars/cockpit-cliproxy");
    let output_dir = sidecar_dir.join("bin");

    println!("cargo:rerun-if-env-changed=XIASS_SKIP_CLIPROXY_BUILD");
    // Keep the legacy environment variable as a read-only build compatibility alias.
    println!("cargo:rerun-if-env-changed=COCKPIT_SKIP_CLIPROXY_BUILD");
    emit_sidecar_rerun_inputs(&sidecar_dir);
    std::fs::create_dir_all(&output_dir).expect("failed to create xiass-cliproxy bin dir");

    if cfg!(target_os = "macos") && target == "universal-apple-darwin" {
        build_macos_universal_sidecar(&sidecar_dir, &output_dir);
        return;
    }

    let Some((goos, goarch)) = go_target_from_rust_target(&target) else {
        panic!("unsupported sidecar build target: {target}");
    };
    build_go_sidecar(&sidecar_dir, &output_dir, &target, goos, goarch);
    if cfg!(target_os = "macos") && target.contains("apple-darwin") {
        build_macos_universal_sidecar(&sidecar_dir, &output_dir);
    }
}

fn build_xiass_wf_bridge() {
    let manifest_dir =
        PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR is required"));
    let target = std::env::var("TARGET").expect("TARGET is required");
    let repository_root = manifest_dir.join("../..");
    let output_dir = manifest_dir.join("../sidecars/wf-bridge/bin");

    println!("cargo:rerun-if-env-changed=XIASS_SKIP_WF_BRIDGE_BUILD");
    // Read-only build compatibility for local environments created before the rebrand.
    println!("cargo:rerun-if-env-changed=COCKPIT_SKIP_WF_BRIDGE_BUILD");
    std::fs::create_dir_all(&output_dir).expect("failed to create xiass-wf-bridge bin dir");

    if cfg!(target_os = "macos") && target == "universal-apple-darwin" {
        let source_dir = repository_root.join("macos/source");
        emit_wf_bridge_rerun_inputs(&source_dir, &source_dir);
        build_macos_universal_wf_bridge(&source_dir, &output_dir);
        return;
    }

    let Some((goos, goarch)) = go_target_from_rust_target(&target) else {
        panic!("unsupported WF bridge build target: {target}");
    };
    let source_dir = if goos == "windows" {
        repository_root.join("windows/source")
    } else {
        repository_root.join("macos/source")
    };
    emit_wf_bridge_rerun_inputs(&source_dir, &source_dir);

    build_go_wf_bridge(&source_dir, &output_dir, &target, goos, goarch);
    if cfg!(target_os = "macos") && target.contains("apple-darwin") {
        build_macos_universal_wf_bridge(&source_dir, &output_dir);
    }
}

fn main() {
    println!("cargo:rerun-if-changed=build.rs");
    build_xiass_cliproxy_sidecar();
    build_xiass_wf_bridge();

    #[cfg(target_os = "macos")]
    {
        SwiftLinker::new("12.0")
            .with_package("MacosNativeMenuSwift", "native/macos-native-menu")
            .link();
        link_macos_swift_runtime_rpaths();
    }

    tauri_build::build()
}
