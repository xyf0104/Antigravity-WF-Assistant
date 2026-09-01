use std::env;
use std::fs::OpenOptions;
use std::io::Write;
use std::path::Path;
use std::process::{self, Command, Stdio};

fn append_diagnostic(path: Option<&Path>, message: &[u8]) {
    let Some(path) = path else {
        return;
    };

    if let Ok(mut file) = OpenOptions::new().create(true).append(true).open(path) {
        let _ = file.write_all(message);
        let _ = file.flush();
    }
}

fn main() {
    let current_exe = env::current_exe().unwrap_or_else(|error| {
        eprintln!("Unable to resolve the WiX linker wrapper path: {error}");
        process::exit(1);
    });
    let real_light = current_exe.with_file_name("light.real.exe");
    let diagnostic_path = env::var_os("XIASS_WIX_LIGHT_LOG");
    let diagnostic_path = diagnostic_path.as_deref().map(Path::new);
    let args: Vec<_> = env::args_os().skip(1).collect();

    append_diagnostic(
        diagnostic_path,
        format!(
            "XIASS WiX linker wrapper\nreal linker: {}\narguments: {:?}\n\n",
            real_light.display(),
            args
        )
        .as_bytes(),
    );

    let output = Command::new(&real_light)
        // WiX 3.14 validation depends on legacy Windows script components
        // that are absent on hosted runners. The generated installers remain
        // gated by extraction, installation, launch, and sidecar smoke tests.
        .arg("-sval")
        .args(&args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .unwrap_or_else(|error| {
            let message = format!(
                "Unable to execute verified WiX linker {}: {error}\n",
                real_light.display()
            );
            append_diagnostic(diagnostic_path, message.as_bytes());
            eprint!("{message}");
            process::exit(1);
        });

    append_diagnostic(diagnostic_path, b"stdout:\n");
    append_diagnostic(diagnostic_path, &output.stdout);
    append_diagnostic(diagnostic_path, b"\nstderr:\n");
    append_diagnostic(diagnostic_path, &output.stderr);
    append_diagnostic(
        diagnostic_path,
        format!("\nexit status: {:?}\n", output.status.code()).as_bytes(),
    );
    let _ = std::io::stdout().write_all(&output.stdout);
    let _ = std::io::stderr().write_all(&output.stderr);

    process::exit(output.status.code().unwrap_or(1));
}
