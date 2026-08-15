# PowerPilot

PowerPilot is a native Windows application for power automation, conditional tasks, process-aware workflows and live system resource monitoring.

## Highlights

- Shutdown, restart, sleep, hibernate, lock, sign out and other power actions
- Simple and advanced tasks with conditions, delays, process checks and saved scenarios
- CPU, GPU, RAM, disks, network and per-process resource monitoring
- Extended hardware sensors through LibreHardwareMonitor / PawnIO
- Long-term application traffic/activity statistics
- Notifications, tray controls, mini mode, themes and configurable UI behavior
- Built-in one-click updates through GitHub Releases — no full installer wizard for normal updates
- Portable and installer builds for Windows x64

## Updates

Stable releases use tags in the form `vX.Y.Z`. PowerPilot checks the latest GitHub Release and downloads the dedicated `PowerPilot_Update_X.Y.Z.zip` package. The package and its internal application payload are SHA-256 verified before replacement; the updater can roll back if the new application fails its startup check.

See [GITHUB_UPDATES.md](GITHUB_UPDATES.md) for the update/release contract.

Testers can use the rolling `PowerPilot_Develop_Setup.exe` prerelease. That build follows the `develop` branch through a separate update channel and does not affect stable clients.

## Development

PowerPilot is primarily written in Go and uses native Win32 + Direct2D/DirectWrite for the desktop UI.

```bash
python -m pip install -r requirements.txt
python build_release.py
```

The release script builds and validates the Windows x64 application, updater, uninstaller and setup package and generates versioned release assets.

## Third-party components

PowerPilot uses LibreHardwareMonitor and related components for extended hardware sensor access. See [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).

---

**Windows x64** · Native Win32 UI · Power automation · Resource monitoring
