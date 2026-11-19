# Project folder structure

This document describes the Go API's current layout and where new code belongs.

```text
.
├── cmd/api/                 # Application entry point
├── internal/
│   ├── api/                 # HTTP handlers and API tests
│   ├── config/              # Runtime configuration and tests
│   └── store/               # Data access and integration tests
├── db/                      # Database schema
├── deploy/                  # Deployment instructions and service configuration
├── loadtest/                # Load-testing scripts, reports, and diagnostics
│   └── diagnostics/         # Diagnostic scripts and recorded results
├── docs/                    # Project architecture and contributor documentation
├── .github/workflows/       # CI configuration
├── Makefile                 # Development commands
└── README.md                # Project overview
```

## Where to put new files

- Add new executable entry points under `cmd/<app>/`.
- Keep application-only packages under `internal/`, grouped by responsibility.
- Keep database schema changes in `db/`.
- Put deployment assets in `deploy/` and performance tests in `loadtest/`.
- Put cross-cutting architecture notes and contributor guides in `docs/`.
- Place unit tests alongside the Go code they exercise, using `*_test.go`.

## Quality checks

Run `make check` before committing to verify formatting, static analysis, and tests.
