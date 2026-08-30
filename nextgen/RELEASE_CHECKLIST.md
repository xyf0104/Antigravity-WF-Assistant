# XIASS Tools Nextgen Release Checklist

This checklist is the release source of truth for the Nextgen Tauri application. Do not publish
from `macos/source` or `windows/source`, and do not upload local `target/`, portable executables,
source archives, or dependency caches.

## 1. Freeze and validate the release source

1. Stop all source edits and confirm the intended release commit is on `main`.
2. Update the same semantic version in `package.json`, `CHANGELOG.md`, `CHANGELOG.zh-CN.md`, and
   the root README sentence `当前正式版本为 v<version>`. `npm run sync-version` synchronizes
   `src-tauri/Cargo.toml` and `src-tauri/tauri.conf.json` from `package.json`.
3. From `nextgen/`, run:

   ```bash
   npm ci
   npm run release:preflight
   node --test scripts/release/*.test.cjs
   node scripts/check_brand_and_license.cjs
   cargo fmt --all --check
   ```

4. Confirm there are no unexpected generated configs or secrets in the worktree:

   ```bash
   git status --short
   git diff --check
   ```

`src-tauri/tauri.release.generated.conf.json` is generated only during a release build and must
remain ignored. Never commit an updater private key, API key, account file, diagnostic archive, or
generated sidecar binary.

## 2. Configure GitHub Actions secrets

The tag release workflow intentionally fails before building when either required signing secret is
missing:

- `TAURI_UPDATER_PUBLIC_KEY`
- `TAURI_SIGNING_PRIVATE_KEY`
- `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` only when the private key is encrypted

The updater public key is baked into the application. Keep the matching private key outside the
repository and preserve a recoverable offline backup. Losing or replacing it prevents installed
clients from accepting future updates.

Apple Developer ID/notarization and Windows Authenticode are separate optional distribution
credentials. The current workflow produces an ad-hoc-signed macOS application and updater-signed
artifacts; it does not claim Apple notarization or Windows Authenticode.

## 3. Optional platform build validation before tagging

Run the root workflows `Build XIASS Tools Nextgen for macOS` and
`Build XIASS Tools Nextgen for Windows` against the frozen commit. They must produce:

- macOS: one Universal `.app` and one Universal `.dmg`
- Windows: one x64 `.msi` and one x64 NSIS `-setup.exe`

The macOS workflow verifies that the main executable, CLI proxy, and WF bridge contain both
`x86_64` and `arm64`, and that the application passes strict deep code-signature validation. The
Windows workflow verifies both sidecars and both installers exist and are non-empty.

## 4. Publish exactly one tagged release

From the repository root, create the version tag from the frozen release commit and push only that
tag:

```bash
version="$(node -p "require('./nextgen/package.json').version")"
git tag "v${version}"
git push origin "v${version}"
```

The root `Publish XIASS Tools Nextgen Release` workflow then:

1. verifies the tag, package version, both changelogs, and updater credentials;
2. builds macOS Universal and Windows x64 in parallel;
3. stages only approved installer/updater assets;
4. verifies updater signatures and generates target manifests, `latest.json`, and
   `SHA256SUMS.txt`;
5. creates or reuses a draft release;
6. uploads the complete verified asset set and checks the remote asset count; and
7. publishes the draft as the latest release only after every previous step succeeds.

Do not manually upload additional `.zip`, portable executables, raw `.app` directories, source
packages, or build folders. GitHub may display its automatic source-code links; those are generated
by GitHub and are not project release assets.

The three direct-install choices are the Universal macOS DMG, Windows MSI, and Windows NSIS Setup
EXE. The macOS `.app.tar.gz`, signature files, and JSON manifests are internal automatic-update
assets. Tauri's macOS updater requires the signed `.app.tar.gz`, so removing it would disable
in-application updates even though it is not a user-facing installer.

## 5. Verify the published release

After the workflow publishes the draft, run from `nextgen/`:

```bash
version="$(node -p "require('./package.json').version")"
node scripts/release/verify_published_updater_manifests.cjs \
  --version "$version" \
  --repo xyf0104/Antigravity-WF-Assistant \
  --targets darwin-aarch64-app,darwin-x86_64-app,windows-x86_64-msi,windows-x86_64-nsis \
  --legacy true
```

Also verify:

- the Release is public, marked latest, and matches `v<package version>`;
- the DMG, MSI, and Setup EXE download successfully;
- `SHA256SUMS.txt` matches the published primary binaries;
- a previously installed macOS client and Windows client can check, download, install, and restart
  into the new version; and
- fresh install, upgrade, close-to-tray, explicit exit, and uninstall behavior are smoke-tested on
  both operating systems.

If any post-publication validation fails, do not replace assets under the same public version.
Correct the source, increment the version, and publish a new signed release.
