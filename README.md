<div align="center">
  <img src="./assets/logo.svg" alt="Kibotos" width="72" height="72" />
  <h1>Kibotos</h1>
  <p>
    <a href="https://docs.kibo.to"><img src="https://img.shields.io/badge/Docs-docs.kibo.to-0F172A?style=flat-square" alt="Docs" /></a>
    <a href="./sdks/typescript"><img src="https://img.shields.io/badge/TypeScript-%40kibotos%2Fsdk-3178C6?style=flat-square&logo=typescript&logoColor=white" alt="TypeScript SDK" /></a>
    <a href="./sdks/python"><img src="https://img.shields.io/badge/Python-kibotos-3776AB?style=flat-square&logo=python&logoColor=white" alt="Python SDK" /></a>
    <a href="./sdks/go"><img src="https://img.shields.io/badge/Go-sdks%2Fgo-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go SDK" /></a>
    <a href="./cli"><img src="https://img.shields.io/badge/CLI-kibo-111827?style=flat-square" alt="kibo CLI" /></a>
    <a href="./LICENSE"><img src="https://img.shields.io/badge/License-MIT-22C55E?style=flat-square" alt="MIT License" /></a>
  </p>
  <p>Official Community and Developer Ecosystem for Kibotos v1 API</p>
</div>

---

## SDK

Install the official Kibotos v1 SDK for your language:

**Python**

```bash
pip install kibotos
```

**Go**

```bash
go get github.com/felinics/kibotos-sdk/sdks/go
```

**TypeScript / JavaScript**

```bash
npm install @kibotos/sdk
```

For quickstarts, full method references, versioned guides, and v1 API documentation, visit the [Kibotos Documentation](https://docs.kibo.to/en/docs/sdk) (or jump straight to [TypeScript](https://docs.kibo.to/en/docs/sdk/typescript) · [Python](https://docs.kibo.to/en/docs/sdk/python) · [Go](https://docs.kibo.to/en/docs/sdk/go) · [API Reference](https://docs.kibo.to/en/docs/api)).

## Kibotos Client

`kibo` is the official command-line client for Kibotos — built for both humans and AI coding agents.

### Use Kibotos with your agent

Copy and paste the following prompt to your AI coding agent (Claude Code, Codex, Cursor, Antigravity, etc.) to install the **`use-kibo`** skill from this repository:

```text
Please install the `use-kibo` agent skill from https://github.com/felinics/kibotos-sdk (located at `skills/use-kibo/`). After installing the skill, check whether the `kibo` CLI is installed and authenticated by running `kibo whoami --json`, and guide me through installing or logging in if needed.
```

### Install and use `kibo` manually

Install `kibo` with Go (or download a prebuilt binary from [GitHub Releases](https://github.com/felinics/kibotos-sdk/releases)):

```bash
go install github.com/felinics/kibotos-sdk/cli/cmd/kibo@latest
```

Sign in to your Kibotos account (opens a browser authorization flow and saves a local profile) and spin up your first workspace:

```bash
# Authenticate via browser Device Flow (or pass --api-key kb_...)
kibo login

# Verify your active profile and available zones
kibo whoami

# Create a workspace and wait until it is available
kibo workspace create --template python-3.12 --name my-sandbox --wait

# Expose a port over a temporary HTTPS URL
kibo exposure create <workspace_id> --port 8080
```

See the full [Kibotos Client (`kibo`) Documentation](https://docs.kibo.to/en/docs/sdk/cli) for profiles, volumes, network policies, `--json` scripting mode, and exit codes.
