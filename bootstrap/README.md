# PowerPilot source bootstrap

This directory is only a fallback for GitHub Actions while the full source tree is not committed directly to the repository.

Upload a source archive named:

`PowerPilot_<version>_source.zip`

The release workflow restores the newest matching ZIP, verifies that `app/main.go` is present, reads `appVersion`, prepares build assets, builds all Windows x64 artifacts, validates them, and publishes the GitHub Release.

Once the full source tree is committed normally, this bootstrap ZIP can be removed.
