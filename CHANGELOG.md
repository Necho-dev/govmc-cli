# Changelog

**English** | [简体中文](CHANGELOG.zh.md)

Versions follow [SemVer](https://semver.org/). English and Chinese entries share the same version headings. `0.1.0` is the first release.

## [Unreleased]

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
