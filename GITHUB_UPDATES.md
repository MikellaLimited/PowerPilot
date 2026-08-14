# PowerPilot updates via GitHub Releases

The application updater is configured for the public repository:

`MikellaLimited/PowerPilot`

PowerPilot checks GitHub Releases at application startup and every 6 hours while it is running. A manual check is available in Settings -> Data -> PowerPilot updates.

## Release contract

Create a stable GitHub Release tagged `vX.Y.Z` and attach at least:

- `PowerPilot_Setup_X.Y.Z.exe`
- `PowerPilot_Portable_X.Y.Z.exe`
- `PowerPilot_X.Y.Z_source.zip`
- `PowerPilot_X.Y.Z_RELEASE_NOTES.txt`
- `PowerPilot_X.Y.Z_release_validation.json`

The in-app updater downloads the Setup asset and requires the SHA-256 digest returned for the GitHub release asset before launching it.

The repository release workflow can build and publish these files from a `vX.Y.Z` tag. The repository and release assets must remain publicly readable for token-free desktop update checks.

Do not embed a GitHub personal access token in PowerPilot.
