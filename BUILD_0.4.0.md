# PowerPilot 0.4.0 build verification

Release builds are produced with `build_release.py`.

Required checks:
- Windows amd64 compile/test for app, installer and uninstaller.
- `-trimpath -ldflags "-s -w -H windowsgui"` for release binaries.
- No COFF/DWARF debug-symbol growth.
- Installer payload PowerPilot.exe SHA-256 must equal standalone release PowerPilot.exe.
- Final Setup.exe must contain valid 16/24/32/48/64/128/256 32-bit icon resources.
- The Explorer-visible Setup.exe icon must still be visually checked on a real Windows desktop before treating the release as fully visually verified.
