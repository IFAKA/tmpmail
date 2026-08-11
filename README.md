# tmpmail

A disposable-email TUI built for keyboard-first/nvim users.

`tmpmail` creates a temporary Mail.tm inbox, copies the address to your clipboard, polls for incoming mail, and extracts likely OTPs, magic links, verification links, and confirmation actions.

> Mail delivery is provided by [Mail.tm](https://mail.tm/). Disposable email domains are rejected by some services. Do not use temporary mail for accounts you need to recover later.

## Install

### Release binary

```sh
curl -fsSL https://raw.githubusercontent.com/IFAKA/tmpmail/main/install.sh | sh
```

The installer puts `tmpmail` in `~/.local/bin` by default. Override with `TMPMAIL_INSTALL_DIR`.

### Go

```sh
go install github.com/IFAKA/tmpmail/cmd/tmpmail@latest
```

## Use

```sh
tmpmail
```

On startup it creates a mailbox and copies its address.

### Inbox keys

| Key | Action |
|---|---|
| `j` / `k` | next / previous |
| `g` / `G` | first / last |
| `l` / `Enter` | open message |
| `Y` | copy mailbox address |
| `n` | create a new mailbox |
| `r` | refresh |
| `?` | key help |
| `q` | quit |

### Message keys

| Key | Action |
|---|---|
| `h` / `Esc` | back |
| `y` | smart-copy best action |
| `a` | show extracted action candidates |
| `q` | back |

### Candidate picker

`j/k` selects, `y` copies, and `Esc` cancels.

`y` only copies automatically when the best candidate is unambiguous. Otherwise it opens the picker instead of guessing.

## What it detects

The extractor treats useful email actions as ranked candidates rather than assuming every site uses a six-digit OTP:

- numeric OTPs
- separated numeric codes (`123-456`)
- alphanumeric codes (`ABC123`)
- segmented codes (`Q7K-29P`)
- verification URLs
- magic/sign-in URLs
- confirmation URLs
- links hidden behind HTML buttons

Scores are raised by nearby verification/authentication language and reduced for likely invoice, order, tracking, phone-number, date, and unsubscribe noise.

## Edge behavior

- Network failure keeps already-fetched messages visible.
- Empty inbox stays in a waiting state and refreshes automatically.
- Clipboard failure shows the value instead of losing it.
- Multiple plausible OTPs/links open a candidate picker.
- HTML-only mail gets a readable fallback.
- Tiny terminals degrade to a minimum-size message.
- A new mailbox is never silently created because the old one failed.

## Development

Requires Go 1.23+. The TUI runtime intentionally uses only the Go standard library plus common system tools (`stty` and the platform clipboard command), so there are no Go runtime dependencies to download.

```sh
make test
make build
./bin/tmpmail
```

## GitHub Pages

The static site lives in `docs/`. `.github/workflows/pages.yml` deploys it using GitHub Pages Actions. In the repository settings, set Pages source to **GitHub Actions** once.

## Releases

Tag a version:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The release workflow builds Linux/macOS binaries for amd64 and arm64 and attaches `.tar.gz` archives to the GitHub release.

## Architecture

```text
cmd/tmpmail
    ↓
TUI state machine
    ↓
provider.Provider
    ↓
Mail.tm REST API

message MIME/text/HTML
    ↓
action extractor
    ↓
ActionCandidate { type, value, score, evidence }
    ↓
smart copy or explicit candidate picker
```

The provider is behind an interface so another disposable-mail backend can be added without coupling it to the TUI.

## Privacy and limitations

Temporary inboxes are not private long-term identity infrastructure. Messages are delivered through the external provider and the generated mailbox is intentionally disposable. Never use this for sensitive or durable accounts.

## License

MIT.
