# Build pp-mt5 and pp-mt5-mcp with a version stamp.
#
# Usage:
#   .\scripts\build.ps1                  # dev build (0.1.0-dev)
#   .\scripts\build.ps1 -Version v0.1.0  # release build
#   .\scripts\build.ps1 -OutDir .\dist   # output directory (default: bin/)
#
# The version stamp updates github.com/.../internal/cli.Version via -ldflags,
# which the MCP server also reads via mcp.ServerVersion(). One variable, both
# binaries.
[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$OutDir = "bin"
)

$ErrorActionPreference = "Stop"

if (-not $Version) {
    $tag = (git describe --tags --always --dirty 2>$null)
    if ($LASTEXITCODE -eq 0 -and $tag) {
        $Version = $tag
    } else {
        $Version = "0.1.0-dev"
    }
}

$ldflags = "-X github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/cli.Version=$Version"

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

Write-Host "Building pp-mt5  version=$Version → $OutDir\pp-mt5.exe"
& go build -ldflags $ldflags -o (Join-Path $OutDir "pp-mt5.exe") ./cmd/pp-mt5
if ($LASTEXITCODE -ne 0) { throw "build pp-mt5 failed" }

Write-Host "Building pp-mt5-mcp  version=$Version → $OutDir\pp-mt5-mcp.exe"
& go build -ldflags $ldflags -o (Join-Path $OutDir "pp-mt5-mcp.exe") ./cmd/pp-mt5-mcp
if ($LASTEXITCODE -ne 0) { throw "build pp-mt5-mcp failed" }

Write-Host ""
Write-Host "OK. Verify:"
Write-Host "  $OutDir\pp-mt5.exe --version"
Write-Host "  $OutDir\pp-mt5-mcp.exe --version"
