# Changelog

**English** | [简体中文](CHANGELOG.zh.md)

Versions follow [SemVer](https://semver.org/). English and Chinese entries share the same version headings. `0.1.0` is the first release.

## [Unreleased]

## [0.1.1]

### Changed

- Standardize the bundled Skill identifier, source and installation directories, CLI help, UI metadata, translations and README links as `govmc`.
- Reduce the Skill entrypoint to essential operating boundaries and a task router; load nine focused references only as needed. Setup/permissions, Tasks/logs/audits and datastore operations have dedicated references.
- Document complete CLI/stdio MCP clone and transfer workflows: inspect, preview, confirm, follow the persistent task and verify the result. Remove fixed legacy clone specifications; inherit the source configuration unless the user requests overrides.
- Clarify Skill upgrades and installation paths for Codex, Cursor and Claude Code. Updating an executable does not replace an installed Skill; replacing it is explicit and other Skill directories are left intact. MCP tool names continue to follow the actual server schema.

### Fixed

- Include the complete README in Python wheel metadata for the PyPI description, with image and documentation URLs pinned to the matching release tag.

### Validation

- Validate embedded Skill identity, reference links and installation into all three supported clients; check documented CLI command paths/flags and MCP example names/arguments against the implementation without executing device operations.
- Verify native/wheel installation parity, including the `govmc` Skill entrypoint, UI metadata and task-specific references. Distribution tests can also accept an already published wheel.
- Compare installed Skill contents as original UTF-8 bytes and accept LF/CRLF example fences, preventing false failures on Windows. Windows CI covers Skill installation, documented commands and wheel behavior.
- Stabilize the TUI scroll-and-return regression by waiting for the final row without a backlog of key events.
- Run local published-package/WMKS lifecycle checks, Go race/simulator regressions, Linux/Windows CI and browser interaction regressions during 0.1.1 preparation. This validation does not publish a release.

## [0.1.0]

### Initial release

- Native `govmc` CLI, full-screen TUI, stdio MCP and Web workspace, built on VMware `govmomi` and shared Go services.
- ESXi host and machine inventory, power operations, hardware details, screenshots and CPU, memory, network and disk metrics.
- CPU/memory, disk and network configuration with change previews and confirmation; disk capacity can grow only.
- Machine cloning with powered-off source validation, inherited source configuration and disk provisioning, optional destination datastore, dry-run and confirmation.
- Datastore browsing, supported file previews, upload/download, folder creation and same-host cross-datastore copy/move.
- SQLite-backed tasks with list/status/detail/cancellation, step progress and shared CLI/TUI/Web/MCP queries.
- Encrypted Profiles in `~/.govmc/config.yaml`, temporary `--profile` selection and stable history ownership across renames. CLI/TUI provide Profile management; Web switches saved environments.
- TUI Settings for language, MCP permissions, Profiles, tasks, runtime logs and operation audits; version and GitHub footer link.
- Web metrics, machine configuration, datastore browser, task drawer, appearance/language settings, connection status and per-environment IndexedDB caching.
- Optional VMware HTML Console SDK (WMKS), installed separately through `wmks install/status/verify/uninstall`. Ordinary Web works without the SDK; SDK files are excluded from source and release artifacts.
- Interactive Web consoles with fresh-ticket reconnection and navigation/refresh/close confirmation. CLI and stdio MCP support bounded ticket-based keyboard/pointer input.
- Default read-only MCP permissions, explicit write authorization and a bundled offline Skill supporting CLI and stdio MCP.
- Shared English/Chinese dictionaries, readable terminal logs, original JSONL runtime records and SQLite operation audits.
- Native ZIP/SHA-256 releases and additional `govmc-cli` platform wheels containing the same Go executable; tag-driven GitHub/PyPI publishing with bilingual release validation.
- Simulator integration/race tests, browser interaction regressions, SDK installation lifecycle and native/wheel parity checks. README includes simulator-based screenshots and an animated tour.

### Scope and limitations

- Standalone ESXi is the primary target; vCenter host/datacenter selection is limited.
- Local `serve` has no account login or Cookie authentication. Docker account/invitation management is not included.
- TUI offers console screenshots, without interactive keyboard/pointer control. MCP has no general machine-configuration editing tool.
- Tasks and history are shared by processes using the same local configuration/data directory. Cancellation does not roll back changes; interrupted workflows are not automatically replayed.
- Project code is available under the MIT License; third-party components retain their licenses. VMware WMKS remains a separately licensed external dependency excluded from source and releases.
