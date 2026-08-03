# GopherAI Backend

This directory contains our backend implementation. The `GopherAI-v1` and
`GopherAI-v2` directories at the repository root are reference code only.

```text
backend/
├── cmd/server/                 application entry point
├── internal/httpapi/           Gin handlers, middleware, and routes
├── internal/user/              user business module and repository contract
├── internal/platform/postgresql/ PostgreSQL repository implementations
└── migrations/                 database schema migrations
```

The first feature is user registration. Start from the business logic in
`internal/user`; HTTP and PostgreSQL integration are added after that behavior
is covered by tests.

Run the PostgreSQL repository integration test against a migrated test
database:

```bash
TEST_DATABASE_URL='postgres://user:password@127.0.0.1:5432/gopherai_test' \
  go test -tags=integration ./internal/platform/postgresql
```
