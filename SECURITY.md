# Security policy

## Reporting a vulnerability

Use GitHub private vulnerability reporting: <https://github.com/yangflow/oriel/security/advisories/new>. Do not
open a public issue or pull request.

Include the version or commit, the operating system, steps to reproduce and what an attacker gains. Do not include
real passwords, tokens, API keys or reading content.

## Scope

In scope:

- Updates: bypassing update signature or checksum verification, or installing an update from a non-HTTPS source.
- Credentials: the server password or an AI endpoint API key appearing in logs, crash reports or plain-text
  configuration.
- The headless server (`oriel serve`): bypassing sign-in or sessions, or a remote client reaching data without
  authentication.
- User content: feed content that makes the app execute code, read or write arbitrary files, or reach private or
  loopback addresses.

Out of scope:

- Vulnerabilities in third-party services you configure yourself, such as RSSHub or an AI endpoint.
- Attacks that first require local user access.
- Unsigned development builds being unsigned.

## What to expect

One maintainer, in spare time. Expect an acknowledgement within about a week. Confirmed issues are fixed in a new
release with a GitHub security advisory, crediting the reporter by default. There is no bounty. Only the latest
version is supported.
