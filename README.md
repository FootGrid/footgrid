# FootGrid

Backend monorepo for FootGrid's grassroots football platform. The first product
slice is intentionally narrow: match setup, live event logging, pitch lineups,
substitutions, undo, and live/public read models.

## Services

| Deployable | Responsibility | AWS runtime |
| --- | --- | --- |
| `identity-api` | organizations, memberships, players and teams | API Gateway HTTP API + Go Lambda |
| `match-api` | match setup, roster, lineups, live clock and immutable event ledger | API Gateway HTTP API + Go Lambda |
| `projection-worker` | consumes outbox records and produces snapshots, timelines and statistics | EventBridge/SQS + Go Lambda |

The services share one Aurora PostgreSQL cluster initially, but own separate
schemas. No service writes another service's schema.

## Quick start

Prerequisites: Go 1.23+, Docker Desktop, `golang-migrate`, and AWS CLI for deployment.

```bash
cp .env.example .env
docker compose up -d postgres
make migrate-up
make test
make run-match-api
```

The local API listens on `http://localhost:8080`. `AUTH_DISABLED=true` is for
local development only; deployed environments use Cognito JWT verification.

## Commands

```bash
make fmt             # format Go code
make lint            # vet all packages
make test            # unit tests
make test-unit       # unit tests only
make test-integration # requires local Postgres
make migrate-up      # apply database migrations
make migrate-down    # roll back one migration
make openapi-check   # validate OpenAPI YAML is parseable
make build           # compile Lambda binaries
```

## Repository map

```text
api/                  OpenAPI contract
cmd/                  Lambda entry points
internal/             domain/application/adapters, not importable externally
migrations/           forward-only PostgreSQL migrations
infra/terraform/      AWS infrastructure by environment
docs/                 architecture decisions and delivery plan
tests/                black-box integration and contract tests
```

Start with [docs/implementation-plan.md](docs/implementation-plan.md). The
public contract is [api/openapi.yaml](api/openapi.yaml).

### Testing structure

Unit tests remain beside the package they exercise so they can verify domain
behavior and other package-internal contracts. Cross-package tests belong under
`tests/`, where they use public APIs and can run against real infrastructure.

```text
internal/<area>/            production code only
tests/unit/<area>/          package unit tests
tests/integration/          PostgreSQL and service integration tests
tests/contract/             API contract tests (when added)
```

Run `make test` for the fast unit suite. Run `make test-integration` only after
starting PostgreSQL and applying the migrations.

## Source of truth

`footgrid` is the deployable backend. Its versioned migrations and
`api/openapi.yaml` are the canonical database and HTTP contract sources.
The sibling `platform` directory contains browser prototypes and architecture
reference material; it is not a second backend implementation or deployable
service. Keep prototype changes aligned with the canonical contracts before
wiring them to a service.
