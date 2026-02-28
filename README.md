# RateMyLifeDecision

Link-centric decision-rating app with Go API + Next.js frontend.

## Run

1. Bootstrap database (recommended for team setup):

```bash
make dev-up
```

2. Start REST server:

```bash
make run-server
```

Server listens on `http://localhost:8080`.

3. Install frontend dependencies:

```bash
make web-install
```

4. Run frontend:

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

## Environment

Copy `.env.example` values into your shell environment as needed:

- `PORT` (default `8080`)
- `DATABASE_URL` (default local Postgres DSN)
- Frontend env: copy `web/.env.example` to `web/.env.local` if needed.

## Endpoints

- `GET /health` -> `{"ok": true}`
- `POST /api/decisions`
- `GET /api/decisions/{slug}?viewer_id=<uuid>`
- `POST /api/decisions/{slug}/responses`
- `POST /api/responses/{responseID}/vote`

## Frontend Routes

- `/` Create Decision page (generate share link + copy button)
- `/d/{slug}` Decision page (submit response, view stats, vote on responses)

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
    "rating": 4,
    "emoji": "😬",
    "comment": "Do it if you can handle the rent"
  }'
```

Vote on a response:

```bash
curl -X POST http://localhost:8080/api/responses/<response-id>/vote \
  -H "Content-Type: application/json" \
  -d '{
    "viewer_id": "992f80ff-d9d8-47fd-97c0-c140aeb5232a",
    "value": 1
  }'
```
