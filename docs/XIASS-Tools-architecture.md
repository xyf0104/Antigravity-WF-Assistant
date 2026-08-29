# XIASS Tools Architecture

## Product boundary

XIASS Tools is a local, installable configuration companion for the user's
own AI developer tools. It keeps the already-supported Antigravity workflow
and exposes Codex, Claude Code, Cursor, and Windsurf actions only when their
local capability contracts have been verified on macOS and Windows.

The product works locally. A platform adapter may contact an upstream service
only after the user starts a documented action such as OAuth, quota refresh,
or model discovery. It must never upload local credentials, conversations, or
application databases to a XIASS service merely to manage them.

## Source and licensing rule

The Codex configuration helper under the XIASS API repository is first-party
source and can be integrated after it has been adapted to the Wails host.

Cockpit Tools is an external functional reference only. Its current public
licensing and README restrictions do not permit its source code, branding,
layouts, OAuth parameters, icons, or copy to be moved into a commercial
XIASS Tools release. The implementation below is independently written.

## Product layers

```text
Wails UI
  └── Agent registry
        ├── Antigravity adapter: existing verified patch and proxy workflow
        ├── Codex adapter: explicit config.toml management
        ├── Claude Code adapter: explicit settings.json management
        ├── Cursor adapter: documented global MCP configuration only
        └── Windsurf adapter: documented global MCP configuration only
              ├── local installation discovery
              ├── version-gated configuration inspection
              ├── explicit, transactional configuration mutation
              ├── verified backup / restore where implemented
              └── credential-free local diagnostics

Platform secret handling
  ├── request-local API keys and tokens for explicitly initiated writes
  ├── platform secure storage only for the implemented local TOTP feature
  └── no account-store, OAuth, session, database, Cookie or browser-storage import

Common safety services
  ├── private application state
  ├── operation locks
  ├── atomic writes and SHA-256 manifests
  ├── reversible backups
  ├── diagnostics with secret redaction
  └── update verification
```

## Required platform behaviour

| Platform | Detect and diagnose | Supported configuration surface | Account/OAuth/quota | Apply / restore | Launch |
| --- | --- | --- | --- | --- | --- |
| Antigravity | yes | existing verified local proxy and patch workflow | existing product-specific workflow only | existing verified patch backup and restore | existing launcher |
| Codex | yes | independent `xiass_tools` provider in `config.toml`; optional guarded history/workspace repair | not read or claimed | atomic config backup / restore and explicit repairs | discovery only |
| Claude Code | yes | only `settings.json`: API root, authorization token and model | not read or claimed | atomic settings backup / restore | discovery only |
| Cursor | yes | documented global `~/.cursor/mcp.json` only, after its contract is wired and verified | not read or claimed | reserved XIASS MCP entry only, with backup / rollback | discovery only |
| Windsurf | yes | documented global `~/.codeium/windsurf/mcp_config.json` only, after its contract is wired and verified | not read or claimed | reserved XIASS MCP entry only, with backup / rollback | discovery only |

No adapter may claim an account is connected, a quota is available, or a
client version is supported unless the adapter has read and validated the
actual local structure or a documented upstream response.

## Delivery sequence

1. Keep the visible product identity as XIASS Tools and migrate existing
   legacy local state without deleting user data.
2. Add the shared agent registry, discovery contract, transaction contract,
   vault interface, and diagnostics model.
3. Integrate the first-party Codex helper through Wails bindings, including
   config backup, model discovery, history safety repair, workspace repair,
   installation discovery, and explicit restore.
4. Add Claude Code actions only through the three documented user-setting
   fields, with testable backup and restore behavior.
5. Add Cursor and Windsurf only through their documented global MCP JSON
   files; do not inspect or mutate private account storage.
6. Keep the local TOTP vault isolated from third-party account credentials and
   omit it entirely from logs and diagnostic exports.
7. Finish the XIASS design system, native tray behavior, Windows NSIS installer,
   macOS universal installer, update flow, and per-platform smoke tests.

## Verification gate

Every adapter change requires unit coverage for path discovery, malformed
inputs, backup/restore, interrupted mutation recovery, secret redaction, and
unsupported-client rejection. A release also requires native Windows and macOS
builds plus a smoke test against a representative installed client version for
each supported platform. Unsupported structures are shown as unsupported;
they are never force-written.
