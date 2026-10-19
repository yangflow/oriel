# Releasing

Pushing a tag `vX.Y.Z` runs `.github/workflows/release.yml`: the checks, then a draft release, then one build per
platform that signs and uploads its installers and updates to the draft, then publishing the release once every
platform uploaded. Installed copies check the latest published release of `yangflow/oriel` once a day and from
"Check for Updates…" in the app menu, and install nothing before the user confirms in the update window. Only the
maintainer pushes release tags. The update mechanism is MyGo's
([auto-updates](https://github.com/egoist/mygo/blob/v0.3.6/docs/updates.md),
[GitHub Actions](https://github.com/egoist/mygo/blob/v0.3.6/docs/github-actions.md)).

## One-time setup

Credentials go only into the secrets of the environment `release`, never into files, logs, issues or pull requests.
The environment lets only tags `v*` deploy, so a workflow on a branch or a pull request never sees them. Set each
secret with `gh secret set NAME --env release`, from a file or the prompt, never with the value on the command line.

**Update key.** On the maintainer's machine:

```sh
go tool mygo keygen -o <a directory outside the repository>
gh secret set MYGO_UPDATER_PRIVATE_KEY --env release < <that directory>/mygo-update.key
```

Keep `mygo-update.key` in a password manager as well: installed copies accept only updates signed with it, so
losing it strands them. The contents of `mygo-update.pub` go into `updates.publicKey` in `mygo.json`; the public
key is not a secret.

**macOS.** A Developer ID Application certificate of the Apple Developer Program, and an App Store Connect API key
for the notary service (Users and Access, Integrations, Team Keys; the `.p8` downloads once):

| Secret | Value |
|---|---|
| `MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD` | The certificate with its private key, exported from the keychain as a `.p12` with a password; the first is `base64 -i cert.p12`. Delete the file afterwards. |
| `MACOS_SIGNING_IDENTITY` | The certificate's SHA-1 hash, as listed by `security find-identity -v -p codesigning` |
| `NOTARY_KEY_P8_BASE64`, `NOTARY_KEY_ID`, `NOTARY_ISSUER_ID` | `base64 -i AuthKey_<key ID>.p8`, the key ID and the issuer ID of the API key |

The signing identity stays a secret so that the name on the certificate is not in the repository. Notarization is
added to the configuration only in the workflow; local builds are signed ad hoc and never submitted.

**Windows.** Builds are not signed yet, so SmartScreen warns on first launch.

**Linux.** Nothing to set up; the `.deb` and the archives are not signed beyond the update signature.

## Each release

1. Run `python3 scripts/bump.py X.Y.Z`. It sets the version in `mygo.json` and turns `[Unreleased]` into
   `## [X.Y.Z] - <date>` in `CHANGELOG.md`; the update window shows that section. For the first release, give the
   current version: it dates the notes without skipping a version.
2. Open a pull request with the two files (`chore(release): X.Y.Z`) and squash-merge it.
3. Tag the merge commit and push the tag:

   ```sh
   git tag vX.Y.Z <commit>
   git push origin vX.Y.Z
   ```

The workflow stops before building when the tag, the version in `mygo.json` and the changelog section disagree, or
when `updates` is not configured, and before uploading when a secret is missing. A failed platform can be re-run
from the Actions page; it uploads to the same draft, and the release is published only when all three succeed.
