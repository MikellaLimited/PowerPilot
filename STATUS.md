# Repository status

This repository is the canonical public home for PowerPilot source, release metadata and update infrastructure.

## Update channel

PowerPilot checks GitHub Releases from `MikellaLimited/PowerPilot`.

Release tags use `vX.Y.Z` and release assets should include:

- `PowerPilot_Setup_X.Y.Z.exe`
- `PowerPilot_Portable_X.Y.Z.exe`
- `PowerPilot_Update_X.Y.Z.zip` (dedicated in-app update package)
- `PowerPilot_X.Y.Z_source.zip`
- `PowerPilot_X.Y.Z_RELEASE_NOTES.txt`
- `PowerPilot_X.Y.Z_release_validation.json`

The installer is intended for first installation or manual recovery. Normal in-app updates use the dedicated update package with package and payload SHA-256 verification plus automatic rollback.
