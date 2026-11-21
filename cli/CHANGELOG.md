# Changelog

All notable changes to the `kibo` CLI are documented in this file.

## [0.1.0] - 2026-11-21

### Added

- Initial release of `kibo`, the official command-line interface for Kibotos.
- Authentication & multi-profile management:
  - `kibo login` with interactive browser Device Authorization Flow (`--no-browser`, `--device-name`, `--console-url`) and non-interactive `--api-key` (`--skip-verify`).
  - `kibo whoami` (`--offline`), `kibo logout`, and `kibo profile list|use|delete`.
  - Atomic `0600` config storage in `~/.config/kibo/config.json` with flag > environment > profile > default precedence.
- Full Kibotos v1 resource commands with short aliases:
  - `kibo workspace` (`ws`, `workspaces`): `list`, `get`, `create`, `update`, `delete`, `start`, `stop`, `timeout`, `network`, `volume attach|detach`, `wait`.
  - `kibo volume` (`vol`, `volumes`): `list`, `get`, `create`, `update`, `resize`, `delete`, `wait`.
  - `kibo exposure` (`exp`, `exposures`): `list`, `get`, `create`, `revoke`.
  - `kibo zone` (`zones`): `list`.
  - `kibo template` (`tpl`, `templates`): `list`, `get`, `revisions`, `revision`.
  - `kibo version`.
- Scriptable `--json` / `-o json` mode with strictly valid JSON on `stdout`, structured error envelopes on `stderr`, `--wait` / `--wait-timeout` support on async mutations, and semantic exit codes (`0`, `1`, `2`, `3`).
- Paired `use-kibo` Agent Skill (`skills/use-kibo/`) for AI coding agents.
