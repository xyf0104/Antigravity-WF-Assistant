# XIASS Tools third-party notices

XIASS Tools includes open-source components maintained by their respective
authors. Their licenses apply only to those components and do not change the
license of the Cockpit-derived XIASS Tools Nextgen source.

The installer places this file and the referenced license texts in its
`licenses` resource directory so the notices remain available offline after
installation.

## Derived source and bundled sidecars

- Cockpit Tools, imported from `jlcodes99/cockpit-tools` at revision
  `a0508ae815e104e931dae515389e680840008367`, is used under CC BY-NC-SA 4.0.
  See `XIASS-Tools-Nextgen-CC-BY-NC-SA-4.0.txt`,
  `CC-BY-NC-SA-4.0-LEGALCODE.txt`, and `ORIGIN_AND_LICENSE.md` in the same
  directory.
- CLIProxyAPI v7.2.140 (`a7e3596b`) is bundled in the XIASS CLI proxy sidecar
  under the MIT License. See `CLIProxyAPI-MIT.txt` in the same directory.
- The independently developed macOS and Windows WF bridge implementations are
  covered by the XIASS Tools MIT notice. See `XIASS-Tools-MIT.txt`.

## Desktop runtime

- Tauri and the Tauri JavaScript API/plugins: Apache-2.0 OR MIT. See
  `Tauri-APACHE-2.0.txt` and `Tauri-MIT.txt`.
- React and React DOM: MIT. See `React-MIT.txt`.
- jsQR: Apache-2.0. See `jsQR-APACHE-2.0.txt`.
- Lucide: ISC, with Feather-derived portions under MIT. See
  `Lucide-ISC-and-MIT.txt`.
- protobuf.js: BSD-3-Clause. See `protobufjs-BSD-3-Clause.txt`.

Other direct JavaScript runtime packages are distributed under the following
SPDX license expressions recorded by the locked packages:

- MIT: `blueimp-md5`, `clsx`, `daisyui`, `date-fns`, `i18next`, `otpauth`,
  `otplib`, `react-i18next`, `tailwind-merge`, and `zustand`.
- Apache-2.0: `long`.

The native application and its two Go sidecars also link additional crates and
Go modules. Exact versions are pinned in `src-tauri/Cargo.lock`,
`sidecars/cockpit-cliproxy/go.sum`, and the platform `go.sum` files in the
corresponding source distribution. Those components retain their own license
terms. The source distribution is available from:

https://github.com/xyf0104/Antigravity-WF-Assistant

This notice is an attribution and redistribution record, not legal advice or a
claim that third-party authors endorse XIASS Tools.
