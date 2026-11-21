# Changelog

All notable changes to the Kibotos Go SDK (`github.com/felinics/kibotos-sdk/sdks/go`) are documented in this file.

## [0.1.0] - 2026-11-21

### Added

- Initial release of the official zero-dependency Go SDK for the Kibotos v1 API (Go 1.24+).
- `NewClient`, `NewClientFromEnv`, functional `ClientOption`s (`WithBaseURL`, `WithTimeout`, `WithMaxRetries`, `WithRetryBackoff`, `WithUserAgent`, `WithHTTPClient`), and per-request `RequestOption`s (`WithIdempotencyKey`, `WithHeader`).
- Full v1 service coverage:
  - `client.Zones`: `List`, `Iter`
  - `client.Templates`: `List`, `Iter`, `Get`, `ListRevisions`, `GetRevision`
  - `client.Workspaces`: `Create`, `List`, `Iter`, `Get`, `Update`, `Delete`, `Start`, `Stop`, `SetTimeout`, `SetNetwork`, `AttachVolume`, `DetachVolume`, `Wait`
  - `client.Volumes`: `Create`, `List`, `Iter`, `Get`, `Update`, `Resize`, `Delete`, `Wait`
  - `client.Exposures`: `Create`, `List`, `Iter`, `Get`, `Revoke`
- Generic `Nullable[T]` (`Some`/`Null`) PATCH helpers, `Ptr[T]`, `iter.Seq2` pagination iterators, chunked `Wait` for timeouts exceeding 60 seconds, automatic UUID v4 `Idempotency-Key` headers on `POST` mutations, and `*APIError` helper predicates (`IsNotFound`, `IsRetryable`, `IsRateLimited`, `IsWaitTimeout`, `IsUnauthenticated`, `IsUnexpectedStatus`).
