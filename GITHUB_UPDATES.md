# PowerPilot updates via GitHub Releases

PowerPilot uses the public repository `MikellaLimited/PowerPilot` as its release/update source.

The application checks the latest stable GitHub Release at startup and every 6 hours while it is running. A manual check is available in Settings -> Data.

## Release contract

Each stable release is tagged `vX.Y.Z` and publishes:

- `PowerPilot_Setup_X.Y.Z.exe` — first installation / manual repair.
- `PowerPilot_Portable_X.Y.Z.exe` — portable build.
- `PowerPilot_Update_X.Y.Z.zip` — in-app update package.
- `PowerPilot_X.Y.Z_source.zip` — source snapshot.
- `PowerPilot_X.Y.Z_RELEASE_NOTES.txt` — release notes.
- `PowerPilot_X.Y.Z_release_validation.json` — release validation report.

## In-app update flow

1. PowerPilot reads `/releases/latest` from GitHub.
2. If a newer stable version exists, it selects `PowerPilot_Update_X.Y.Z.zip`.
3. The downloaded GitHub asset must have a SHA-256 digest and that digest must match the downloaded package.
4. PowerPilot extracts `PowerPilot.Update.exe` from the verified package and starts it.
5. PowerPilot exits normally, saving its state.
6. The updater verifies `update_manifest.json` and the inner SHA-256 of `PowerPilot.exe`.
7. It backs up the current application, replaces it, starts the new version and performs a short startup check.
8. If the new build exits during that check, the updater rolls back to the previous executable and restarts it.

The normal installation path is `%LOCALAPPDATA%\\Programs\\PowerPilot`, so updates usually do not need UAC. If the user installed PowerPilot in a protected folder such as Program Files, the updater asks for elevation only for the file replacement step.

No GitHub personal access token is embedded in PowerPilot. The repository and stable release assets must remain publicly readable for token-free update checks.
