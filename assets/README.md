# README media

TUI and ordinary Web captures use local simulator/fixture data. Web screenshots use the dark theme. The console screenshot is an actual connected WMKS session with a framebuffer-only CSS blur for privacy; environment and machine labels are anonymized in the capture browser. Public images exclude credentials and environment addresses. WMKS SDK files are never included.

The 800×60 CLI banner types example commands with a blinking cursor, following the OpenWrt CLI banner. It contains no command output, TUI screens or Web screens.

```sh
# Requires Pillow in a temporary Python environment.
python build/media/cli.py --font /path/to/JetBrainsMonoNerdFont-Regular.ttf
```

Regenerate terminal captures from the repository root:

```sh
GOVMC_TUI_SIM_TEST=1 GOVMC_TUI_CAPTURE_DIR=/tmp/govmc_media go test -count=1 -run '^TestTUISimulatorPagesAndEnvironment$' ./internal/tui
python build/media/render.py --captures /tmp/govmc_media
```

The renderer accepts `--font` and `--cjk-font`. Use a regular Nerd Font to match the GitHub icon proportion; cells are rendered at their original aspect ratio.

`make web-smoke` generates ordinary Web captures. Console presentation must show the real connected UI, rather than a test iframe or SDK installation guide. Review every image for confidential content before adding it.
