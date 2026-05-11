# aggr

`aggr` is a small Go HTTP server that fronts multiple OpenAI-compatible providers behind one OpenAI-like API.

## What it does

- Stores provider config in SQLite.
- Syncs each provider's `/v1/models` catalog.
- Aggregates `GET /v1/models` across enabled providers.
- Routes other `/v1/*` requests by the `model` field in the request.
- Serves a Vue-based Web UI for provider management.

## Run it

### Backend

```sh
go run ./server/cmd/aggr
```

The server listens on `:8080` by default and stores data in `aggr.db`.

### Web UI in development

Run the backend:

```sh
AGGR_ENV=dev AGGR_WEB_DEV_URL=http://127.0.0.1:5173 go run ./server/cmd/aggr
```

Run the Vite dev server in a second terminal:

```sh
pnpm --dir web dev
```

You can visit either:

- `http://127.0.0.1:5173` directly, with Vite proxying API calls to the backend
- `http://127.0.0.1:8080`, with the Go server reverse proxying the Vite UI

### Production bundle

Use the repo build target so the embedded HTML is always regenerated before the Go binary is built:

```sh
make build
```

That runs `pnpm --dir web build` first and writes a single self-contained HTML file to `server/internal/webui/dist/index.html`, which is then embedded into the Go binary.

If you want to run the steps manually, build the Web UI before any Go build:

```sh
pnpm --dir web build
go build ./server/cmd/aggr
```

## Environment variables

- `AGGR_ADDR`: server listen address, default `:8080`
- `AGGR_DB_PATH`: SQLite file path, default `aggr.db`
- `AGGR_ENV`: set to `dev` to default the UI to the Vite dev server
- `AGGR_WEB_DEV_URL`: optional Vite dev server URL, default `http://127.0.0.1:5173` when `AGGR_ENV=dev`
- `VITE_API_PROXY_TARGET`: optional Vite proxy target, default `http://127.0.0.1:8080`
