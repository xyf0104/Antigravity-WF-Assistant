# Changelog

## [1.7.1] - 2026-08-31

### Added

- Added one unified Chinese-first workspace for each of the five primary Agents: Antigravity WF, Codex, Claude Code, Cursor and Windsurf.
- Added a complete in-app manual for the Antigravity proxy, model, image, account, patch, diagnostics and launch workflow.
- Added release tests for UI accessibility, configuration persistence, Tauri command wiring, explicit exit behavior and real frontend readiness.

### Changed

- Redesigned the shell and embedded workspaces with the XIASS deep-sea background, transparent brand mark, rounded liquid-glass surfaces and consistent light, dark and system themes.
- Standardized buttons, tabs, dialogs and form actions on a 44 px interaction target with explicit hover, focus, active, disabled and loading states.
- Improved responsive wrapping, long Chinese labels, URL fields, modal layering and Codex session controls at narrow workspace widths and 125% UI scale.
- Updated the macOS lifecycle so closing the window keeps the service available in the background while explicit Quit closes the bridge and releases its ports.

### Fixed

- Prevented provider and account configuration reads from silently replacing existing data after malformed or failed storage responses.
- Fixed the WF bridge shutdown path so stdin closure stops the proxy and releases local listeners.
- Fixed macOS CI/updater startup configuration and added a React commit readiness marker so an alive process without a mounted interface cannot pass release smoke tests.
- Isolated Rust tests from shared environment-variable state so the full workspace test suite passes with the default parallel runner.
- Fixed dashboard title contrast, remote MCP field clipping, small or unnamed controls, modal accessibility and first-frame theme flashes.

## [1.7.0] - 2026-08-31

### Added

- Unified local account management for Antigravity, Codex, Claude Code, Cursor, Windsurf and other supported clients.
- OAuth authorization flows with automatic callback handling and manual callback fallback for supported providers.
- Secure local account storage, quota display, account testing, import/export and instance management.

### Changed

- Rebranded the desktop application, data directories and public service defaults as XIASS Tools.
- Preserved read-only compatibility with legacy Cockpit account data, shortcuts, extension storage and protocol identifiers.

### Fixed

- Removed upstream advertisement, announcement and private remote-configuration requests.
- Improved cross-platform installer migration and OAuth launch progress reporting.
