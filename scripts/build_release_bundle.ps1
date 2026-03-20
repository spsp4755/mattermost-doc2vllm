$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
$workspaceRoot = Split-Path -Parent $repoRoot

function Add-ToolPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PathToAdd
    )

    if ((Test-Path $PathToAdd) -and -not (($env:PATH -split ';') -contains $PathToAdd)) {
        $env:PATH = "$PathToAdd;$env:PATH"
    }
}

Add-ToolPath (Join-Path $repoRoot 'tools\go\bin')
Add-ToolPath (Join-Path $repoRoot 'tools\node')
Add-ToolPath (Join-Path $workspaceRoot 'tools\go\bin')
Add-ToolPath (Join-Path $workspaceRoot 'tools\node')

$env:GOCACHE = Join-Path $repoRoot '.gocache'
$env:GOMODCACHE = Join-Path $repoRoot '.gomodcache'
$env:GOPATH = Join-Path $repoRoot '.gopath'

Push-Location $repoRoot

try {
    & go.exe run ./build/manifest apply
    if ($LASTEXITCODE -ne 0) {
        throw 'manifest apply failed'
    }

    $manifest = Get-Content -Raw -Path (Join-Path $repoRoot 'plugin.json') | ConvertFrom-Json
    $pluginId = $manifest.id
    $version = $manifest.version

    Remove-Item -Recurse -Force (Join-Path $repoRoot 'server\dist') -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $repoRoot 'webapp\dist') -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force (Join-Path $repoRoot 'dist') -ErrorAction SilentlyContinue

    New-Item -ItemType Directory -Path (Join-Path $repoRoot 'server\dist') | Out-Null

    $targets = @(
        @{GOOS = 'linux'; GOARCH = 'amd64'; Output = 'plugin-linux-amd64'},
        @{GOOS = 'linux'; GOARCH = 'arm64'; Output = 'plugin-linux-arm64'},
        @{GOOS = 'darwin'; GOARCH = 'amd64'; Output = 'plugin-darwin-amd64'},
        @{GOOS = 'darwin'; GOARCH = 'arm64'; Output = 'plugin-darwin-arm64'},
        @{GOOS = 'windows'; GOARCH = 'amd64'; Output = 'plugin-windows-amd64.exe'}
    )

    foreach ($target in $targets) {
        $env:CGO_ENABLED = '0'
        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH
        & go.exe build -trimpath -o (Join-Path $repoRoot "server\dist\$($target.Output)") ./server
        if ($LASTEXITCODE -ne 0) {
            throw "server build failed for $($target.GOOS)-$($target.GOARCH)"
        }
    }

    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue

    Push-Location (Join-Path $repoRoot 'webapp')
    try {
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) {
            throw 'webapp build failed'
        }
    } finally {
        Pop-Location
    }

    $pluginRoot = Join-Path $repoRoot "dist\$pluginId"
    New-Item -ItemType Directory -Path $pluginRoot | Out-Null

    & go.exe run ./build/manifest dist
    if ($LASTEXITCODE -ne 0) {
        throw 'manifest dist failed'
    }

    if (Test-Path (Join-Path $repoRoot 'assets')) {
        Copy-Item -Recurse -Path (Join-Path $repoRoot 'assets') -Destination $pluginRoot
    }
    if (Test-Path (Join-Path $repoRoot 'public')) {
        Copy-Item -Recurse -Path (Join-Path $repoRoot 'public') -Destination $pluginRoot
    }

    New-Item -ItemType Directory -Path (Join-Path $pluginRoot 'server') | Out-Null
    Copy-Item -Recurse -Path (Join-Path $repoRoot 'server\dist') -Destination (Join-Path $pluginRoot 'server')

    New-Item -ItemType Directory -Path (Join-Path $pluginRoot 'webapp') | Out-Null
    Copy-Item -Recurse -Path (Join-Path $repoRoot 'webapp\dist') -Destination (Join-Path $pluginRoot 'webapp')

    & go.exe run ./build/package
    if ($LASTEXITCODE -ne 0) {
        throw 'package build failed'
    }

    $bundlePath = Join-Path $repoRoot "dist\$pluginId-$version.tar.gz"
    Write-Host "Bundle created: $bundlePath"
} finally {
    Pop-Location
}
