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
        ├── Codex adapter: explicit config.toml management and guarded Desktop lifecycle
        ├── Claude Code adapter: explicit settings.json management
        ├── Cursor adapter: documented global MCP and explicitly selected project MCP configuration
        └── Windsurf adapter: documented global MCP configuration only
              ├── local installation discovery
              ├── version-gated configuration inspection
              ├── explicit, transactional configuration mutation
              ├── verified backup / restore where implemented
              └── credential-free local diagnostics

Platform secret handling
  ├── request-local API keys and tokens for explicitly initiated writes
  ├── user-pasted, account-level API/OAuth transport credentials only through a strict field allow-list
  ├── platform secure storage only for the implemented local TOTP feature
  └── no external app account-store, client_secret, session, database, Cookie or browser-storage import

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
| Codex | yes | independent `xiass_tools` provider in `config.toml`; optional guarded history/workspace repair | may reuse an enabled static OpenAI Responses account without exposing its credential to the renderer; native OAuth/session/quota are not imported | atomic config backup / restore and explicit repairs | only for a structure-verified Codex / ChatGPT Desktop installation, after explicit user confirmation; graceful stop/start/restart only, never force-kill |
| Claude Code | yes | only `settings.json`: API root, authorization token and model | may reuse an enabled static Anthropic Messages account without exposing its credential to the renderer; OAuth/refresh/session/quota are not imported | atomic settings backup / restore | terminal lifecycle is intentionally not managed |
| Cursor | yes | documented global `~/.cursor/mcp.json`, plus `<explicitly selected project>/.cursor/mcp.json` | not read or claimed | reserved XIASS MCP entry only, with atomic write, verified recovery points, explicit restore and explicit deletion | may open a freshly re-verified installation; no account or process-session management |
| Windsurf | yes | documented global `~/.codeium/windsurf/mcp_config.json` only | not read or claimed | reserved XIASS MCP entry only, with atomic write, verified recovery points, explicit restore and explicit deletion | may open a freshly re-verified installation; no account or process-session management |

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
5. Add Cursor only through its documented global MCP JSON or an explicitly
   user-selected project `.cursor/mcp.json`; add Windsurf only through its
   documented global MCP JSON. Do not inspect or mutate private account storage.
6. Keep the local TOTP vault isolated from third-party account credentials,
   support only user-initiated encrypted export/import with password, integrity
   validation and rollback, and omit it entirely from logs and diagnostic exports.
7. Finish the XIASS design system, native tray behavior, Windows NSIS installer,
   macOS universal installer, update flow, and per-platform smoke tests.

## Verification gate

Every adapter change requires unit coverage for path discovery, malformed
inputs, backup/restore, interrupted mutation recovery, secret redaction, and
unsupported-client rejection. A release also requires native Windows and macOS
builds plus a smoke test against a representative installed client version for
each supported platform. Unsupported structures are shown as unsupported;
they are never force-written.
