# Local Testing Setup

This workspace is prepared to build and test the Mattermost LLM plugin locally on Windows.

## Installed Toolchains

- Go: `C:\Users\USER\Documents\Playground\tools\go`
- Node.js/npm: `C:\Users\USER\Documents\Playground\tools\node`
- Python: `C:\Users\USER\Documents\Playground\tools\python313`

## Server Checks

```powershell
$env:GOCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-build'
$env:GOMODCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-mod'
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' test ./server/...
```

## Webapp Checks

Type check:

```powershell
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' run check-types
```

Tests:

```powershell
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' test -- --runInBand
```

Build:

```powershell
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' run build
```

## Full Bundle Build

```powershell
$env:GOCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-build'
$env:GOMODCACHE='C:\Users\USER\Documents\Playground\repo\.codex-cache\go-mod'
$env:npm_config_cache='C:\Users\USER\Documents\Playground\repo\.codex-cache\npm'
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' run ./build/manifest apply
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' test ./server/...
Push-Location webapp
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' run check-types
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' test -- --runInBand
& 'C:\Users\USER\Documents\Playground\tools\node\npm.cmd' run build
Pop-Location
& 'C:\Users\USER\Documents\Playground\tools\go\bin\go.exe' build -trimpath -o server\dist\plugin-windows-amd64.exe .\server
```

## Offline / Closed-Network Validation

- Build the plugin bundle once in this workspace.
- Verify the final runtime bundle is `dist\com.mattermost.vllm-llm-<version>.tar.gz`.
- Deploy that tarball into Mattermost in the target closed network.
- Ensure the configured LLM endpoint is reachable from inside the closed network.
- If scanned PDFs must be supported, install one of the documented PDF rasterizers on the Mattermost plugin host.
