# taskthing bootstrap installer (Windows).
#
#   irm https://raw.githubusercontent.com/costaluu/taskthing/master/scripts/install.ps1 | iex
#
# Downloads the latest release binary into a per-user location, adds it to the
# user PATH, and tells you to run `taskthing install`. It deliberately does NOT
# run the interactive first-run setup itself: piped into `iex` there is no
# reliable console to drive the prompts (Spec 0005).
#
# Overrides: $env:TASKTHING_REPO (owner/repo), $env:TASKTHING_BIN_DIR (install dir).
$ErrorActionPreference = "Stop"

$Repo = if ($env:TASKTHING_REPO) { $env:TASKTHING_REPO } else { "costaluu/taskthing" }
$InstallDir = if ($env:TASKTHING_BIN_DIR) { $env:TASKTHING_BIN_DIR } else { Join-Path $env:LOCALAPPDATA "taskthing\bin" }

# Only a 64-bit Windows binary is shipped (Bun compiles win32-x64 only).
if (-not [Environment]::Is64BitOperatingSystem) {
  throw "taskthing: needs 64-bit Windows"
}
$asset = "taskthing-win32-x64.exe"

# The `latest/download` redirect resolves the newest release and serves the named
# asset — no API call, no JSON parsing, no rate limit.
$url = "https://github.com/$Repo/releases/latest/download/$asset"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$target = Join-Path $InstallDir "taskthing.exe"

Write-Host "taskthing: downloading $asset from $Repo..."
# Download to a temp beside the target, then move into place, so a failed
# download never leaves a half-written binary behind.
$tmp = "$target.download"
try {
  Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
} catch {
  if (Test-Path $tmp) { Remove-Item $tmp -Force }
  throw "taskthing: could not download $asset from the latest $Repo release"
}
Move-Item -Path $tmp -Destination $target -Force

# Add the install dir to the user PATH if it isn't already there.
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ";") -notcontains $InstallDir) {
  $newPath = if ([string]::IsNullOrEmpty($userPath)) { $InstallDir } else { "$userPath;$InstallDir" }
  [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
  Write-Host "taskthing: added $InstallDir to your user PATH — restart your shell"
}

Write-Host "taskthing: installed to $target"
Write-Host 'taskthing: now run `taskthing install`'
