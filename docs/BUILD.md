# Building PowerPilot

PowerPilot release builds target Windows x64 and require Go 1.23, Python 3.12 or newer, and the Python packages listed in `requirements.txt`.

Install Python dependencies and run the release builder:

```powershell
python -m pip install -r requirements.txt
python build_release.py
```

When the repository-local `.tools/go` toolchain and `.venv` environment are present, the equivalent convenience command is:

```powershell
.\build_local.ps1
```

The builder compiles and tests the application, updater, installer, and uninstaller; patches Windows icon resources; verifies PE structure and size limits; checks the installer payload hash; and creates the versioned Setup, Portable, Update, source, release-notes, and validation artifacts in `release/`.

Before publishing a release, also perform the Windows Explorer icon check and a real install/update/rollback cycle recorded by the validation report.
