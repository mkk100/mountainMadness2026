# RateMyLifeDecision

Link-centric decision-rating app with Go API + Next.js frontend.

## Run

1. Create local env file:

```bash
cp .env.example .env
```

2. Bootstrap database in Docker (recommended for team setup):

```bash
make dev-up
```

3. Start REST server:

```bash
make run-server
```

Server listens on `http://localhost:8080`.

4. Install frontend dependencies:

```bash
make web-install
```

5. Run frontend:

```bash
make run-web
```

Frontend runs on `http://localhost:3000`.

## Database Helpers

Useful Docker DB commands:

```bash
make db-up      # start postgres container
make db-down    # stop containers
make db-logs    # stream postgres logs
make db-shell   # open psql shell inside postgres container
make db-reset   # drop volume, recreate DB, rerun migrations (destructive)
```

If your laptop already uses Postgres on `5432`, change `POSTGRES_HOST_PORT` in `.env` (for example `5433`) and set `DATABASE_URL` to match that port.

## Environment

`.env` drives both Docker Postgres and app runtime via Make:

- `PORT` (default `8080`)
- `POSTGRES_DB`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_HOST_PORT`
- `DATABASE_URL` (must match `POSTGRES_HOST_PORT`)
- `CORS_ALLOWED_ORIGINS` (comma-separated allowlist; defaults to localhost frontend origins)
- `TRUST_PROXY_HEADERS` (`true` when running behind a trusted proxy that sets real client IP headers)
- `WRITE_API_KEYS` (optional comma-separated keys for write-endpoint key rotation; keep empty for local public writes)
- Frontend env: copy `web/.env.example` to `web/.env.local` if needed.

## Endpoints

- `GET /health` -> `{"ok": true}`
- `POST /api/decisions`
- `GET /api/decisions/{slug}?viewer_id=<uuid>`
- `POST /api/decisions/{slug}/responses`
- `POST /api/decisions/{slug}/vote`

## Frontend Routes

- `/` Create Decision page (generate share link + copy button)
- `/d/{slug}` Decision page (submit one response, view stats/comments, vote on post)

## Example Requests

Create a decision:

```bash
curl -X POST http://localhost:8080/api/decisions \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Should I move to Toronto?",
    "description": "New job, expensive rent.",
    "closes_at": null
  }'
```

Get a decision + stats + responses:

```bash
curl "http://localhost:8080/api/decisions/<slug>?viewer_id=9e3d295a-9dd7-4e04-9385-d6aa0b4ce3f2"
```

Submit a response:

```bash
curl -X POST http://localhost:8080/api/decisions/<slug>/responses \
  -H "Content-Type: application/json" \
  -d '{
    "viewer_id": "9e3d295a-9dd7-4e04-9385-d6aa0b4ce3f2",
    "rating": 0,
    "suggestion": 3,
    "emoji": "😬",
    "comment": "Do it if you can handle the rent"
  }'
```

Vote on the decision post:

```bash
curl -X POST http://localhost:8080/api/decisions/<slug>/vote \
  -H "Content-Type: application/json" \
  -d '{
    "viewer_id": "992f80ff-d9d8-47fd-97c0-c140aeb5232a",
    "value": 1
  }'
```
