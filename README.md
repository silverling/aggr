# aggr

`aggr` is a small Go HTTP server that fronts multiple OpenAI-compatible providers behind one OpenAI-like API.

## What it does

- Stores provider config in SQLite.
- Syncs each provider's `/v1/models` catalog.
- Aggregates `GET /v1/models` across enabled providers.
- Routes other `/v1/*` requests by the `model` field in the request.
- Supports Responses API websocket mode on `GET /v1/responses` and routes the
  connection by the first `response.create` model on that socket.
- Serves a Vue-based Web UI for provider management.

## Run it

## Install

Install the latest release on a Linux host with systemd in one line:

```sh
curl -fsSL https://raw.githubusercontent.com/silverling/aggr/main/install.sh | sudo bash
```

That downloads the latest GitHub release, installs `aggr` to `/opt/aggr`,
installs the systemd unit, generates `AGGR_ACCESS_KEY` with
`openssl rand -hex 32` when needed, and starts the service.

To install from a fork instead, set `AGGR_GITHUB_REPO=owner/repo` before
running the installer.

After install:

- The systemd service is `aggr`
- The environment file is `/opt/aggr/.env`
- The generated access key is printed once by the installer
- Check status with `sudo systemctl status aggr`

### Backend

```sh
go run ./server/cmd/aggr
```

The server listens on `:8080` by default and stores data in `aggr.db`.
Set `AGGR_ACCESS_KEY` in your `.env` file before starting the server.

The binary also supports:

```sh
./aggr --help
./aggr --version
./aggr upgrade
```

### Web UI in development

Run the backend:

```sh
AGGR_ENV=dev go run ./server/cmd/aggr
```

Run the Vite dev server in a second terminal:

```sh
pnpm --dir web dev
```

Visit `http://127.0.0.1:5173`, with Vite proxying API calls to the backend.

### Production bundle

Use the repo build target so the embedded HTML is always regenerated before the Go binary is built:

```sh
make build
```

That runs `pnpm --dir web build` first, writes a single self-contained HTML file to `server/internal/webui/dist/index.html`, and builds the Go binary with an embedded git-derived version string.

Version formatting follows the current repository state at build time:

- Exact tag build: `v1.2.3`
- Commit after the nearest reachable tag: `v1.2.3-e10cc320`
- No reachable tag: `e10cc320`
- Dirty worktree: append `-dirty`

If you want to run the steps manually, build the Web UI before any Go build:

```sh
pnpm --dir web build
go build ./server/cmd/aggr
```

### Upgrade an installed binary

Run:

```sh
aggr upgrade
```

That downloads the latest GitHub release for the current platform and replaces
the current executable in place. If `aggr` is running under systemd, restart it
afterward:

```sh
sudo systemctl restart aggr
```

Set `AGGR_GITHUB_REPO=owner/repo` first if the binary should upgrade from a
different GitHub repository.

## Changelog

Release notes are generated from commit messages between tags in the release
workflow and published as the GitHub Release body.

## Environment variables

- `AGGR_ADDR`: server listen address, default `:8080`
- `AGGR_DB_PATH`: SQLite file path, default `aggr.db`
- `AGGR_ACCESS_KEY`: shared access key required for the Web UI and admin APIs
- `AGGR_ENV`: optional runtime label, for example `dev` during local development
- `VITE_API_PROXY_TARGET`: optional Vite proxy target, default `http://127.0.0.1:8080`

After you sign in to the Web UI with the shared access key, create gateway API
keys there and use them as `Authorization: Bearer ...` credentials for the
server's `/v1` endpoints.
