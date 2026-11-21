# Kibotos SDK, CLI & Agent Skills

Official community & developer ecosystem for **Kibotos** (Kibo) v1 API:

- **Go SDK** (`sdks/go`) — Idiomatic Go client (`github.com/felinics/kibotos-sdk/sdks/go`)
- **TypeScript / JavaScript SDK** (`sdks/typescript`) — Zero-dependency `@kibotos/sdk` built on standard `fetch`
- **Python SDK** (`sdks/python`) — Synchronous & asynchronous `kibotos` client built on `httpx` and Pydantic v2
- **Official CLI (`kibo`)** (`cli/`) — Interactive `kibo login` and scriptable `--json` / `--wait` resource management
- **Agent Skill (`use-kibo`)** (`skills/use-kibo/`) — Skill teaching AI coding agents how to operate Kibo workspaces via the `kibo` CLI

## Repository Layout

```text
.
├── cli/                  # Official Go CLI (`kibo`)
├── sdks/
│   ├── go/               # Go SDK
│   ├── typescript/       # TypeScript / JavaScript SDK (@kibotos/sdk)
│   └── python/           # Python SDK (kibotos)
├── skills/
│   └── use-kibo/         # Agent Skill (`use-kibo`)
├── openapi/              # Frozen v1 API & Device Auth contract / OpenAPI snapshot
└── examples/             # Runnable quickstart examples
```

## License

MIT
