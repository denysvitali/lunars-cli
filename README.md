# lunars-cli

Command-line downloads for authorized files on [lunars.dev](https://lunars.dev/).

The site gates its firmware APIs behind a GitHub Sponsors session. This Go CLI uses the same authenticated endpoints as the web app and requires a valid lunars.dev session cookie.

## Usage

```sh
go run . list --search 2024.26
go run . limit
go run . auth check
go run . download 2024.26.8 -o firmware.bin
go run . download 2026.8 --type mcu2 --pick-latest
go run . download latest --type mcu2 -o latest.mcu2
go run . download 2026.8.3 latest --type mcu2 -o ./downloads/
go run . download https://files.lunars.dev/model-s.html
go run . config init --bucket my-bucket --prefix lunars/
go run . config set s3.bucket my-bucket
go run . upload s3 2024.26.8 --bucket my-bucket
```

Pass authentication with either a raw Cookie header or a cookie export:

```sh
LUNARS_COOKIE='__Secure-next-auth.session-token=...' go run . list
go run . --cookie-file ./cookies.txt download 2024.26.8
```

`--cookie-file` accepts either a raw Cookie header saved to a file or a Netscape cookie export containing `lunars.dev` cookies.
It also accepts a file containing only the `__Secure-next-auth.session-token` value.

If neither flag is set, the CLI also looks for a session in:

```text
./.lunars-token
./.lunars-cookie
./cookies.txt
$XDG_CONFIG_HOME/lunars/token
$XDG_CONFIG_HOME/lunars/cookie
$XDG_CONFIG_HOME/lunars/cookies.txt
```

## Configuration

The CLI reads optional configuration from:

```text
$XDG_CONFIG_HOME/lunars/config.yaml
```

If `XDG_CONFIG_HOME` is not set, it uses the platform default user config directory, such as `~/.config/lunars/config.yaml` on Linux.

Print the active path or create the file:

```sh
go run . config path
go run . config init --bucket my-bucket --prefix lunars/
go run . config show
go run . config set s3.bucket my-bucket
go run . config set s3.endpoint-url https://s3.example.com
```

Example:

```yaml
s3:
  bucket: my-bucket
  prefix: lunars/
  endpoint-url: https://s3.example.com
  region: auto
  path-style: true
```

Flags override config file values. You can also use `--config /path/to/config.yaml`.

## Commands

- `list`: show firmware records from `/api/signature`
- `limit`: show monthly download usage from `/api/limit`
- `auth check`: validate the configured session and show quota
- `download <target> [target...]`: sign and download by firmware version, signature, `latest`/`newest`, archive path, or `files.lunars.dev` URL
  - `--type`: filter by archive extension (`mcu2`, `ape3`, …)
  - `--pick-latest`: when a partial query matches multiple versions, take the newest
  - multiple targets require `--output` to be an existing directory (or omit it)
- `config path`: show the XDG config file path
- `config init`: write S3 defaults to the XDG config file
- `config show`: show effective S3 config
- `config set <key> <value>`: update one S3 config value
- `upload s3 <target>`: stream a signed download directly into S3-compatible storage
- `completion <shell>`: generate shell completions for bash, zsh, fish, or PowerShell

Download targets are resolved through `/api/sign-url?path=...`, matching the web app's download flow. Existing files are not overwritten unless `--force` is supplied.
Downloads are written to a `.part` file first and renamed after a successful copy. Use `--resume` to continue an existing `.part` file when the server honors HTTP range requests.

`upload s3` is dry-run by default. In dry-run mode it resolves the target and prints the S3 destination without requesting a signed URL, so it does not consume download quota. Add `--execute` to perform the signed download and upload:

```sh
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... AWS_REGION=auto \
  go run . --cookie-file /tmp/lunars-token upload s3 2024.26.8 \
  --bucket my-bucket \
  --endpoint-url https://s3.example.com \
  --prefix lunars/ \
  --execute \
  --yes
```

Without `--yes`, `upload s3 --execute` prompts before it requests the signed URL.

## Development

```sh
golangci-lint run
go test ./...
go build -o lunars .
```

The CLI uses cobra, viper, logrus, and lipgloss.

GitHub Actions runs golangci-lint, tests, builds the CLI, and runs govulncheck. Pushing a `v*` tag runs GoReleaser and publishes release archives.
