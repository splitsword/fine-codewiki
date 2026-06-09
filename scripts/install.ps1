#Requires -Version 5.1
$ErrorActionPreference = "Stop"

# Enable TLS 1.2 for GitHub API calls on older Windows versions
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Repo = "splitsword/fine-codewiki"
$Binary = "codewiki.exe"

# Detect architecture
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    "x86"   { "386" }
    default {
        Write-Error "Unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)"
        exit 1
    }
}

$OS = "windows"

# Determine install directory
$InstallDir = "$env:LOCALAPPDATA\Programs\codewiki"
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

Write-Host "Installing ${Binary} for ${OS}/${Arch}..."

# Fetch latest release tag
$Latest = $null
try {
    $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/${Repo}/releases/latest" -TimeoutSec 10
    $Latest = $Release.tag_name
} catch {
    Write-Host "Failed to fetch latest release. Will fallback to go install if available."
}

if (-not $Latest -or $Latest -eq "null") {
    Write-Host "No release found. Falling back to go install..."
    go install "github.com/${Repo}/cmd/codewiki@latest"
    Write-Host "Installed via go install."
    exit 0
}

Write-Host "Latest release: ${Latest}"

# Download asset
$AssetName = "codewiki-${Latest}-${OS}-${Arch}.zip"
$DownloadUrl = "https://github.com/${Repo}/releases/download/${Latest}/${AssetName}"
$TempDir = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null

try {
    Write-Host "Downloading ${AssetName}..."
    try {
        Invoke-WebRequest -Uri $DownloadUrl -OutFile "$TempDir\$AssetName" -TimeoutSec 60 -UseBasicParsing
    } catch {
        Write-Host "Download failed. Falling back to go install..."
        go install "github.com/${Repo}/cmd/codewiki@latest"
        Write-Host "Installed via go install."
        exit 0
    }

    Write-Host "Extracting..."
    Expand-Archive -Path "$TempDir\$AssetName" -DestinationPath $TempDir -Force

    Move-Item -Path "$TempDir\codewiki.exe" -Destination "$InstallDir\codewiki.exe" -Force

    Write-Host "Installed codewiki.exe to ${InstallDir}\codewiki.exe"

    # Auto-add to user PATH if not already present
    $currentUserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($InstallDir -notin ($currentUserPath -split ";")) {
        [Environment]::SetEnvironmentVariable("PATH", "$currentUserPath;$InstallDir", "User")
        Write-Host "已将 $InstallDir 添加到用户 PATH"
        $env:PATH = "$env:PATH;$InstallDir"
    }
} finally {
    Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
}
