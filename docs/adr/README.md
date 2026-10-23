# Architecture Decision Records

## Index

| No. | Decision | Status |
|---|---|---|
| [0001](0001-pure-go-native-ui.md) | Pure Go native UI | accepted |
| [0002](0002-standalone-client-and-server.md) | A standalone app: one program is both the client and the server | accepted |
| [0003](0003-pure-go-sqlite.md) | Pure Go SQLite | accepted |
| [0004](0004-webview-for-official-video-players.md) | A WebView only for the official video players | accepted |

## Record format

Each new decision gets a new file named `NNNN-short-title.md`: a four-digit number, lowercase words joined by
hyphens. When a decision is overturned, keep the old file and mark it "superseded by NNNN".

```markdown
# NNNN Title

- Status: proposed | accepted | superseded by NNNN
- Date: YYYY-MM-DD
- Related: optional links to the docs the decision affects

## Context
## Decision
## Cost
## When to revisit
```
