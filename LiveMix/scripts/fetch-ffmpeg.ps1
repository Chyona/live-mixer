<#
.SYNOPSIS
    下载 LiveMix Studio 打包所需的静态 FFmpeg 二进制（Windows）。

.DESCRIPTION
    二进制不会提交到仓库。CI 与本地打包在编译前运行本脚本，把 ffmpeg/ffprobe
    放到 third_party\ffmpeg\windows-x64\bin，随后由 CMake 复制到应用输出目录。

.PARAMETER OutputDir
    输出平台目录，默认 third_party\ffmpeg\windows-x64。

.PARAMETER Force
    重新下载并覆盖已有文件。

.EXAMPLE
    .\scripts\fetch-ffmpeg.ps1

.EXAMPLE
    .\scripts\fetch-ffmpeg.ps1 -Force
#>
[CmdletBinding()]
param(
    [string]$OutputDir = '',
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

# Pinned ffmpeg-static release (currently FFmpeg 6.1.1). Keep the tag and the
# SHA-256 values below in sync; never silently follow a moving "latest" URL.
$release = 'b6.1.1'
$assets = @(
    @{ Name = 'ffmpeg-win32-x64.gz';  Output = 'ffmpeg.exe';  Sha256 = '8883a3dffbd0a16cf4ef95206ea05283f78908dbfb118f73c83f4951dcc06d77' },
    @{ Name = 'ffprobe-win32-x64.gz'; Output = 'ffprobe.exe'; Sha256 = 'f309e6223ad89d2fe54bccd420a7709b66fd27540674e92309578ed491a43c8d' }
)
$license = @{
    Name = 'win32-x64.LICENSE'
    Output = 'ffmpeg-LICENSE.txt'
    Sha256 = '8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903'
}
$buildReadme = @{
    Name = 'win32-x64.README'
    Output = 'ffmpeg-BUILD-README.txt'
    Sha256 = 'a636a7183c58006351acbaf35303c0ed85c6e1320fd4e80de453ba6157de6311'
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path $repoRoot 'third_party\ffmpeg\windows-x64'
} elseif (-not [System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir = Join-Path $repoRoot $OutputDir
}
$OutputDir = [System.IO.Path]::GetFullPath($OutputDir)
$binDir = Join-Path $OutputDir 'bin'

function Write-Step {
    param([string]$Message)
    Write-Host ''
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Invoke-Download {
    param([string]$Uri, [string]$OutFile)

    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            Invoke-WebRequest -Uri $Uri -OutFile $OutFile -UseBasicParsing
            return
        } catch {
            if ($attempt -eq 3) {
                throw "下载失败: $Uri`n$($_.Exception.Message)"
            }
            Start-Sleep -Seconds 2
        }
    }
}

function Expand-GzipFile {
    param([string]$Source, [string]$Destination)

    $inputStream = [System.IO.File]::OpenRead($Source)
    try {
        $gzipStream = New-Object System.IO.Compression.GzipStream -ArgumentList $inputStream, ([System.IO.Compression.CompressionMode]::Decompress)
        try {
            $outputStream = [System.IO.File]::Create($Destination)
            try {
                $gzipStream.CopyTo($outputStream)
            } finally {
                $outputStream.Dispose()
            }
        } finally {
            $gzipStream.Dispose()
        }
    } finally {
        $inputStream.Dispose()
    }
}

function Assert-Checksum {
    param([string]$Path, [string]$Expected, [string]$Label)

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    if ($actual -ne $Expected) {
        throw "SHA-256 校验失败: $Label（期望 $Expected，实际 $actual）"
    }
}

# 用 Process 直接运行并读取退出码，不要写成
#   & $exe -version | Select-Object -First 1
# 后者会让 Select-Object 提前终止管道并杀掉 native 进程，$LASTEXITCODE 可能变成
# -1 或 $null（在 CI 上尤其明显），把正常的二进制误判为“无法运行”。
function Get-ToolVersion {
    param([string]$Exe, [string]$Label)

    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $Exe
    $startInfo.Arguments = '-version'
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = [System.Diagnostics.Process]::Start($startInfo)
    try {
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        $process.WaitForExit()
        $stdout = $stdoutTask.Result
        $stderr = $stderrTask.Result
        $exitCode = $process.ExitCode
    } finally {
        $process.Dispose()
    }

    if ($exitCode -ne 0) {
        $detail = (@($stdout, $stderr) -join "`n").Trim()
        $hint = ''
        if ($exitCode -eq -1073741515) {
            $hint = '（0xC0000135：缺少运行库 DLL，二进制可能不是完全静态链接）'
        } elseif ($exitCode -eq -1073741790) {
            $hint = '（0xC0000022：拒绝访问，可能被杀毒软件拦截）'
        }
        throw "无法运行 $Label（退出码 $exitCode）$hint`n$detail"
    }

    return ($stdout -split "`r?`n" | Where-Object { $_.Trim() } | Select-Object -First 1)
}

$missing = @($assets | Where-Object {
    -not (Test-Path -LiteralPath (Join-Path $binDir $_.Output))
})
$licensePath = Join-Path $binDir $license.Output
$buildReadmePath = Join-Path $binDir $buildReadme.Output

if (-not $Force -and
    $missing.Count -eq 0 -and
    (Test-Path -LiteralPath $licensePath) -and
    (Test-Path -LiteralPath $buildReadmePath)) {
    Write-Step "FFmpeg 已存在: $binDir"
    exit 0
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ('livemix-ffmpeg-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

try {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null

    foreach ($asset in $assets) {
        $url = "https://github.com/eugeneware/ffmpeg-static/releases/download/$release/$($asset.Name)"
        $archive = Join-Path $tempDir $asset.Name
        Write-Step "下载 $($asset.Name)"
        Invoke-Download -Uri $url -OutFile $archive
        Assert-Checksum -Path $archive -Expected $asset.Sha256 -Label $asset.Name

        $destination = Join-Path $binDir $asset.Output
        Expand-GzipFile -Source $archive -Destination $destination
        Write-Host "  已安装 $destination"
    }

    $licenseUrl = "https://github.com/eugeneware/ffmpeg-static/releases/download/$release/$($license.Name)"
    $licenseArchive = Join-Path $tempDir $license.Name
    Write-Step "下载 $($license.Name)"
    Invoke-Download -Uri $licenseUrl -OutFile $licenseArchive
    Assert-Checksum -Path $licenseArchive -Expected $license.Sha256 -Label $license.Name
    Copy-Item -LiteralPath $licenseArchive -Destination $licensePath -Force

    $readmeUrl = "https://github.com/eugeneware/ffmpeg-static/releases/download/$release/$($buildReadme.Name)"
    $readmeArchive = Join-Path $tempDir $buildReadme.Name
    Write-Step "下载 $($buildReadme.Name)"
    Invoke-Download -Uri $readmeUrl -OutFile $readmeArchive
    Assert-Checksum -Path $readmeArchive -Expected $buildReadme.Sha256 -Label $buildReadme.Name
    Copy-Item -LiteralPath $readmeArchive -Destination $buildReadmePath -Force

    $sourceNote = @"
FFmpeg static binaries (ffmpeg, ffprobe)
Release: https://github.com/eugeneware/ffmpeg-static/releases/tag/$release
Upstream source: https://ffmpeg.org/releases/ffmpeg-6.1.1.tar.xz
License: see ffmpeg-LICENSE.txt
Build details: see ffmpeg-BUILD-README.txt

These executables are bundled as separate programs. They are invoked as
subprocesses and are not linked into LiveMix Studio.
"@
    Set-Content -LiteralPath (Join-Path $binDir 'ffmpeg-SOURCE.txt') -Value $sourceNote -Encoding UTF8

    $ffmpegVersion = Get-ToolVersion -Exe (Join-Path $binDir 'ffmpeg.exe') -Label 'ffmpeg.exe'
    [void](Get-ToolVersion -Exe (Join-Path $binDir 'ffprobe.exe') -Label 'ffprobe.exe')

    Write-Step 'FFmpeg 准备完成'
    Write-Host "  输出目录: $binDir"
    Write-Host "  版本:     $ffmpegVersion"
}
finally {
    Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
