# Third-party software

| Name | Version | License | Shipped in the app | Notice | Credited in About |
|---|---|---|---|---|---|
| github.com/egoist/mygo | v0.5.5 | MIT | yes | `resources/ThirdPartyLicenses/mygo/LICENSE` | no |
| github.com/ebitengine/purego | v0.11.1 | Apache-2.0 | yes | `resources/ThirdPartyLicenses/purego/LICENSE` | no |
| github.com/go-text/typesetting | v0.3.5 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/typesetting/LICENSE` | no |
| golang.org/x/image | v0.46.0 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/image/LICENSE` | no |
| github.com/dustin/go-humanize | v1.0.1 | MIT | yes | `resources/ThirdPartyLicenses/humanize/LICENSE` | no |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/uuid/LICENSE` | no |
| github.com/mattn/go-isatty | v0.0.24 | MIT | yes | `resources/ThirdPartyLicenses/isatty/LICENSE` | no |
| github.com/ncruces/go-strftime | v1.0.0 | MIT | yes | `resources/ThirdPartyLicenses/strftime/LICENSE` | no |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/bigfft/LICENSE` | no |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/sys/LICENSE` | no |
| modernc.org/libc | v1.77.1 | BSD-3-Clause, with bundled third-party notices | yes | `resources/ThirdPartyLicenses/libc/` | no |
| modernc.org/mathutil | v1.7.1 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/mathutil/LICENSE` | no |
| modernc.org/memory | v1.12.1 | BSD-3-Clause, with bundled third-party notices | yes | `resources/ThirdPartyLicenses/memory/` | no |
| modernc.org/sqlite | v1.60.1 | BSD-3-Clause; SQLite is public domain; sqlite-vec is MIT | yes | `resources/ThirdPartyLicenses/sqlite/` | no |
| github.com/mmcdole/gofeed | v1.5.0 | MIT | yes | `resources/ThirdPartyLicenses/gofeed/LICENSE` | no |
| github.com/mmcdole/goxpp/v2 | v2.0.0 | MIT | yes | `resources/ThirdPartyLicenses/goxpp/LICENSE` | no |
| golang.org/x/net | v0.61.0 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/net/LICENSE` | no |
| golang.org/x/text | v0.43.0 | BSD-3-Clause | yes | `resources/ThirdPartyLicenses/text/LICENSE` | no |
| codeberg.org/readeck/go-readability/v2 | v2.1.3 | MIT | yes | `resources/ThirdPartyLicenses/readability/LICENSE` | no |
| github.com/andybalholm/cascadia | v1.3.5 | BSD-2-Clause | yes | `resources/ThirdPartyLicenses/cascadia/LICENSE` | no |
| github.com/go-shiori/dom | v0.0.0-20230515143342-73569d674e1c | MIT | yes | `resources/ThirdPartyLicenses/dom/LICENSE` | no |
| github.com/gogs/chardet | v0.0.0-20211120154057-b7413eaefb8f | MIT, with ICU notice | yes | `resources/ThirdPartyLicenses/chardet/` | no |
| github.com/itlightning/dateparse | v0.2.1 | MIT | yes | `resources/ThirdPartyLicenses/dateparse/LICENSE` | no |
| github.com/abadojack/whatlanggo | v1.0.1 | MIT | yes | `resources/ThirdPartyLicenses/whatlanggo/LICENSE` | no |
| Lucide icons (lucide-static, copied into `internal/ui/icons/lucide/`) | 1.59.0 | ISC; icons from Feather are MIT | yes | `resources/ThirdPartyLicenses/lucide/LICENSE` | no |

## Notes

The MyGo CLI is a module-pinned build tool in the same module version; it does not ship.
Transitive modules are listed because their code ships. Directories with several
notices keep every `LICENSE*` file the module carries; the memory logo license is omitted
because the logo is not shipped. Go standard-library components
retain their Go license; a copy ships under resources/ThirdPartyLicenses/go/.
The container image (`Dockerfile`, built from source, not published) adds the runtime base
`gcr.io/distroless/base-debian12`, a Debian 12 subset (glibc, CA certificates, tzdata and others) under its own
packages' licenses, which the base keeps in `/usr/share/doc/<package>/copyright`; the program in it is the one above.

## Documentation

`docs/design.md` and `design/` summarize, in our own words, the UI documentation of MyGo v0.5.5 (MIT License,
Copyright (c) egoist): <https://github.com/egoist/mygo/tree/v0.5.5/docs/ui>. No upstream text or code is shipped in
the app.
