# RateMyLifeDecision

Link-centric decision-rating app backend in Go + Postgres.

## Run

1. Start Postgres:

```bash
make db-up
```

2. Apply migrations:

```bash
make migrate-up
```

3. Start REST server:

```bash
make run-server
```

Server listens on `http://localhost:8080`.

## Environment

Copy `.env.example` values into your shell environment as needed:

- `PORT` (default `8080`)
- `DATABASE_URL` (default local Postgres DSN)

## Endpoints

- `GET /health` -> `{"ok": true}`
- `POST /api/decisions`
- `GET /api/decisions/{slug}`
- `POST /api/decisions/{slug}/responses`
- `POST /api/responses/{responseID}/votes`

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
curl -X POST http://localhost:8080/api/responses/<response-id>/votes \
  -H "Content-Type: application/json" \
  -d '{
    "voter_viewer_id": "992f80ff-d9d8-47fd-97c0-c140aeb5232a",
    "value": 1
  }'
```
