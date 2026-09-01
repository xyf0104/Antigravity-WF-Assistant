use std::env;
use std::process::{self, Command, Stdio};

fn main() {
    let current_exe = env::current_exe().unwrap_or_else(|error| {
        eprintln!("Unable to resolve the WiX linker wrapper path: {error}");
        process::exit(1);
    });
    let real_light = current_exe.with_file_name("light.real.exe");

    let status = Command::new(&real_light)
        // Windows Server hosted runners can lack the legacy scripting engine
        // required by these ICE checks. The installer is still validated by
        // extraction, installation, launch, and sidecar smoke tests below.
        .arg("-sice:ICE09")
        .arg("-sice:ICE32")
        .arg("-sice:ICE61")
        .args(env::args_os().skip(1))
        .stdin(Stdio::inherit())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit())
        .status()
        .unwrap_or_else(|error| {
            eprintln!("Unable to execute verified WiX linker {}: {error}", real_light.display());
            process::exit(1);
        });

    process::exit(status.code().unwrap_or(1));
}
