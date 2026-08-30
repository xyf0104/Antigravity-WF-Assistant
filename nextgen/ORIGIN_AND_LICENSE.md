# XIASS Tools Nextgen — Source origin and license

This directory is derived from **Cockpit Tools** by `jlcodes99`:

- Upstream repository: <https://github.com/jlcodes99/cockpit-tools>
- Imported upstream revision: `a0508ae815e104e931dae515389e680840008367`
- Import date: 2026-08-30
- Upstream license declaration: CC BY-NC-SA 4.0
- License text: <https://creativecommons.org/licenses/by-nc-sa/4.0/legalcode>

The imported source is used for the personal, non-commercial XIASS Tools
project under the attribution, non-commercial and share-alike conditions of
CC BY-NC-SA 4.0. Upstream authorship is not removed by the XIASS product
branding.

## XIASS modifications

The XIASS version is being modified to:

- use the XIASS Tools product name, logo, bundle identifiers and data roots;
- expose only Antigravity WF, Codex, Claude Code, Cursor and Windsurf;
- integrate the independently developed Antigravity WF proxy/patch/image
  runtime and XIASS Codex Helper;
- remove upstream advertisements, affiliate content, announcements, remote
  configuration, update endpoints and author-controlled service defaults;
- replace provider credentials and updater infrastructure with XIASS-owned or
  user-supplied configuration;
- harden local credential storage, 2FA export, backup encryption and LAN
  report authentication before those features are enabled;
- build and test separate macOS and Windows installers.

The vendored CLIProxyAPI subtree is included under
`sidecars/cockpit-cliproxy/third_party/CLIProxyAPI`. Its separate MIT license,
upstream version record and notices are preserved in that subtree. The outer
XIASS sidecar integration remains covered by the Cockpit-derived source notice
above.
