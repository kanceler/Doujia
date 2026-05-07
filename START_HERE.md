# Doujia handoff

This package is source-only by default. It intentionally does not include
generated folders such as `node_modules`, `dist`, runtime databases, logs, or
local cache files. Recreate them on the target machine with the commands below.

## Requirements

- Windows PowerShell
- Node.js 22 or newer
- npm 10 or newer
- Docker Desktop
- Optional: Go 1.25 or newer if you want to run the backend from source

## Start backend

Option A: use the included Windows binary:

```powershell
cd Doujia_clean_source_20260504_175507\Doujia
New-Item -ItemType Directory -Force .\runtime\devflow, .\runtime\workspaces | Out-Null
.\devflow-server.exe -addr 127.0.0.1:18080 -db ".\runtime\devflow\state.db" -projects ".\runtime\workspaces" -agent-mode real
```

Option B: run from Go source:

```powershell
cd Doujia_clean_source_20260504_175507\Doujia
New-Item -ItemType Directory -Force .\runtime\devflow, .\runtime\workspaces | Out-Null
go run .\cmd\devflow-server -addr 127.0.0.1:18080 -db ".\runtime\devflow\state.db" -projects ".\runtime\workspaces" -agent-mode real
```

## Start frontend

Open another PowerShell window:

```powershell
cd code
npm.cmd install
npm.cmd run dev:client -- --host 127.0.0.1 --port 4173 --strictPort
```

Then open:

```text
http://127.0.0.1:4173/client/
```

Backend health check:

```text
http://127.0.0.1:18080/healthz
```

## Notes

- Use `npm.cmd` in PowerShell if plain `npm` is blocked by script execution policy.
- The frontend lock file `code/package-lock.json` is included, so dependency versions are reproducible.
- Vite is configured to ignore `dist` during development, and production builds now clear old `dist/client` output before writing new files.
