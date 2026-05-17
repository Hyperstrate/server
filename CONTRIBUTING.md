# Contributing

Thanks for helping improve Hyperstrate.

## Development

1. Fork the repository.
2. Create a feature branch: `git checkout -b feat/my-change`.
3. Make focused changes with tests where behavior changes.
4. Run the relevant checks:

```bash
go test ./...
go build ./cmd/api ./cmd/worker ./cmd/migrate
```

5. Open a pull request and describe the behavior change, tests, and any migration or security impact.

## Pull Request Guidelines

- Keep pull requests focused on one topic.
- Include tests for bug fixes and behavior changes.
- Regenerate Swagger docs with `make swagger` after changing handlers or DTOs.
- Do not commit `.env`, local databases, build artifacts, API keys, or provider credentials.
- Open an issue before large architectural changes so maintainers can discuss the direction.

## Code Style

Follow the existing module layout under `internal/modules/<name>/`: domain, application, infrastructure, interfaces, and module wiring.
