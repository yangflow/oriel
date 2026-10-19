# Releasing

`.github/workflows/release-please.yml` runs release-please on every push to `main`. It keeps one pull request,
`chore(release): X.Y.Z`, open with the next version in `mygo.json` and its section in `CHANGELOG.md`, both made from
the Conventional Commit titles merged since the last release (`release-please-config.json`): before 1.0, `feat`
raises the minor version and `fix` and `perf` the patch; `feat` lists under Added, `fix` under Fixed, `perf` under
Performance, and the other types are left out. Do not edit `CHANGELOG.md` or the version by hand on `main`; only
the maintainer merges the release pull request.

Merging it creates a draft release with that section as its notes and the tag `vX.Y.Z`. The tag runs
`.github/workflows/release.yml`: the checks, then one build per platform that signs and uploads its installers and
updates to the draft, then publishing the release once every platform uploaded. Installed copies check the latest
published release of `yangflow/oriel` once a day and from "Check for Updates…" in the app menu, and install nothing
before the user confirms in the update window, which shows the version's section of `CHANGELOG.md`. The update mechanism is MyGo's
([auto-updates](https://github.com/egoist/mygo/blob/v0.4.1/docs/updates.md),
[GitHub Actions](https://github.com/egoist/mygo/blob/v0.4.1/docs/github-actions.md)).

## One-time setup

**Release App.** release-please acts as a GitHub App, because a tag or pull request made with the workflow's own
`GITHUB_TOKEN` starts no workflows: the tag would not start `release.yml`, and CI would not run on the release pull
request. Create a GitHub App owned by the maintainer, with no webhook, repository permissions Contents and Pull
requests set to Read and write and nothing else, installed on `yangflow/oriel` only (the App made for idset can be
installed here too). Then store its client ID and a private key generated on the App's page as repository secrets
(not in the environment `release`, which only tags may use):

```sh
gh secret set RELEASE_APP_CLIENT_ID --repo yangflow/oriel
gh secret set RELEASE_APP_PRIVATE_KEY --repo yangflow/oriel < <the downloaded .pem>
```

Delete the `.pem` afterwards; a new key can be generated on the App's page.

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

1. Make sure the titles merged since the last release say what changed for users: the `feat`, `fix` and `perf`
   titles become the release notes and the text of the update window. To change the wording, edit `CHANGELOG.md` on
   the release pull request's branch before merging; release-please keeps the edit until a new commit reaches
   `main`, and then writes the section again.
2. Squash-merge the release pull request once CI passes. The tag and the draft release follow; watch `release.yml`
   on the Actions page.

The workflow stops before building when the tag, the version in `mygo.json` and the changelog section disagree, or
when `updates` is not configured, and before uploading when a secret is missing. A failed platform can be re-run
from the Actions page; it uploads to the same draft, and the release is published only when all three succeed.

If release-please cannot run, a release can still be made by hand: set the version in `mygo.json` and
`.release-please-manifest.json`, add its section to `CHANGELOG.md`, merge that, then tag the merge commit and push
the tag (`git tag vX.Y.Z <commit>`, `git push origin vX.Y.Z`); `release.yml` creates the draft itself.
