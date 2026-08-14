# PowerPilot

PowerPilot is a native Windows application for power automation, conditional tasks, process-aware workflows, and live system resource monitoring.

## Highlights

- Power actions: shutdown, restart, sleep, hibernate, lock, sign out and more
- Simple and advanced tasks with conditions, delays, process checks and reusable saved tasks
- CPU, GPU, RAM, disk, network and hardware-sensor monitoring
- Per-process resource monitoring and long-term activity/traffic statistics
- Hardware sensors powered by LibreHardwareMonitor/PawnIO integration
- Notifications, tray controls, mini mode, themes and configurable UI behavior
- Built-in update checks via GitHub Releases
- Portable and installer builds for Windows x64

## Development

PowerPilot is primarily written in Go and uses native Win32 + Direct2D/DirectWrite for the desktop UI.

### Build

```bash
python build_release.py
```

The release script cross-builds the Windows x64 application, uninstaller and setup package, patches PE icons, validates the installer payload, and writes artifacts to `release/`.

## Releases

Stable releases use tags in the form `vX.Y.Z`. The GitHub Actions workflow in `.github/workflows/release.yml` builds and publishes the Setup, Portable executable, source archive, release notes and validation report.

See [`GITHUB_UPDATES.md`](GITHUB_UPDATES.md) for the updater/release contract.

## Third-party components

PowerPilot uses LibreHardwareMonitor and related components for extended hardware sensor access. See [`THIRD_PARTY_NOTICES.txt`](THIRD_PARTY_NOTICES.txt).

---

**Windows x64** · Native Win32 UI · Power automation · Resource monitoring
