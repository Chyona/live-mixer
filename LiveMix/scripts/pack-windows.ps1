<#
.SYNOPSIS
    编译 LiveMix Studio 并生成 Windows 安装包（NSIS）。

.DESCRIPTION
    流程：编译 Release → windeployqt 部署 Qt 运行库 → 复制 ffmpeg/ffprobe →
    NSIS 生成安装包。ffmpeg 二进制默认从 third_party\ffmpeg\windows-x64\bin
    读取，不会提交到仓库；缺少时请先运行 scripts\fetch-ffmpeg.ps1。

.PARAMETER Configuration
    Release 或 RelWithDebInfo，默认 Release。安装包只支持非 Debug 构建。

.PARAMETER BuildDir
    构建目录，默认 build。

.PARAMETER QtPath
    Qt 6 安装目录；省略时由构建脚本自动查找。

.PARAMETER FFmpegDir
    ffmpeg.exe / ffprobe.exe 所在目录。省略时依次检查环境变量
    LIVEMIX_FFMPEG_DIR 和 third_party\ffmpeg\windows-x64\bin。

.PARAMETER OutputDir
    安装包输出目录，默认 dist。

.PARAMETER Version
    覆盖应用版本号（如 1.2.3），会写入安装包文件名和安装器元数据。
    省略时使用构建目录中 CMake 解析出的版本（生产 tag 或 1.0.0）。

.PARAMETER Clean
    编译前清理构建目录。

.PARAMETER SkipBuild
    跳过编译，直接打包已有的构建产物。

.PARAMETER Jobs
    并行编译线程数。

.EXAMPLE
    .\scripts\pack-windows.ps1

.EXAMPLE
    .\scripts\pack-windows.ps1 -Clean -QtPath C:\Qt\6.8.3\msvc2022_64
#>
[CmdletBinding()]
param(
    [ValidateSet('Release', 'RelWithDebInfo')]
    [string]$Configuration = 'Release',

    [string]$BuildDir = 'build',
    [string]$QtPath = '',
    [string]$FFmpegDir = '',
    [string]$OutputDir = 'dist',
    [string]$Version = '',
    [switch]$Clean,
    [switch]$SkipBuild,
    [int]$Jobs = 0
)

$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

$Version = $Version.Trim()
if ($Version) {
    $Version = $Version -replace '^[vV]', ''
    if ($Version -notmatch '^[0-9]+(\.[0-9]+){0,3}$') {
        throw "无效的版本号：${Version}（应形如 1.2.3）。"
    }
}

function Write-Step {
    param([string]$Message)
    Write-Host ''
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Resolve-RepoPath {
    param([string]$Path)
    if ([System.IO.Path]::IsPathRooted($Path)) {
        return [System.IO.Path]::GetFullPath($Path)
    }
    return [System.IO.Path]::GetFullPath((Join-Path $repoRoot $Path))
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

# --- resolve ffmpeg ---------------------------------------------------------

if ([string]::IsNullOrWhiteSpace($FFmpegDir) -and
    -not [string]::IsNullOrWhiteSpace($env:LIVEMIX_FFMPEG_DIR)) {
    $FFmpegDir = $env:LIVEMIX_FFMPEG_DIR
}
if ([string]::IsNullOrWhiteSpace($FFmpegDir)) {
    $FFmpegDir = Join-Path $repoRoot 'third_party\ffmpeg\windows-x64\bin'
}
$FFmpegDir = Resolve-RepoPath $FFmpegDir

foreach ($name in @('ffmpeg.exe', 'ffprobe.exe')) {
    $path = Join-Path $FFmpegDir $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "未找到 $name。请先运行 scripts\fetch-ffmpeg.ps1，或使用 -FFmpegDir 指定目录：$FFmpegDir"
    }
}
$env:LIVEMIX_FFMPEG_DIR = $FFmpegDir

# --- build ------------------------------------------------------------------

$buildPath = Resolve-RepoPath $BuildDir
if (-not $SkipBuild) {
    $buildArgs = @{
        Configuration = $Configuration
        BuildDir = $buildPath
        FFmpegDir = $FFmpegDir
    }
    if (-not [string]::IsNullOrWhiteSpace($QtPath)) {
        $buildArgs.QtPath = $QtPath
    }
    if ($Clean) {
        $buildArgs.Clean = $true
    }
    if ($Jobs -gt 0) {
        $buildArgs.Jobs = $Jobs
    }
    if ($Version) {
        $buildArgs.ExtraCMakeArgs = @("-DLIVEMIX_APP_VERSION=$Version")
    }

    & (Join-Path $PSScriptRoot 'build.ps1') @buildArgs
    if ($LASTEXITCODE -ne 0) {
        throw "编译失败（退出码 $LASTEXITCODE）。"
    }
}

$exePath = Join-Path $buildPath "$Configuration\LiveMixStudio.exe"
if (-not (Test-Path -LiteralPath $exePath)) {
    $found = Get-ChildItem -LiteralPath $buildPath -Filter 'LiveMixStudio.exe' -File -Recurse -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($found) {
        $exePath = $found.FullName
    }
}
if (-not (Test-Path -LiteralPath $exePath)) {
    throw "未找到 LiveMixStudio.exe：$buildPath"
}

# --- locate windeployqt -----------------------------------------------------

$windeployqt = $null
$candidates = @()
if (-not [string]::IsNullOrWhiteSpace($QtPath)) {
    $candidates += Join-Path $QtPath 'bin\windeployqt.exe'
}
foreach ($qtEnv in @($env:QT_ROOT_DIR, $env:QT_ROOT, $env:QTDIR)) {
    if (-not [string]::IsNullOrWhiteSpace($qtEnv)) {
        $candidates += Join-Path $qtEnv 'bin\windeployqt.exe'
    }
}
foreach ($candidate in $candidates) {
    if (Test-Path -LiteralPath $candidate) {
        $windeployqt = $candidate
        break
    }
}
if (-not $windeployqt) {
    $command = Get-Command windeployqt.exe -ErrorAction SilentlyContinue
    if ($command) {
        $windeployqt = $command.Source
    }
}
if (-not $windeployqt) {
    throw '未找到 windeployqt.exe。请使用 -QtPath 指定 Qt 安装目录，或把 Qt bin 加入 PATH。'
}

# --- stage application ------------------------------------------------------

$outputPath = Resolve-RepoPath $OutputDir
$stageDir = Join-Path $outputPath 'windows-stage'
if (Test-Path -LiteralPath $stageDir) {
    Remove-Item -LiteralPath $stageDir -Recurse -Force
}
New-Item -ItemType Directory -Path $stageDir -Force | Out-Null

Write-Step '复制应用与 FFmpeg'
Copy-Item -LiteralPath $exePath -Destination $stageDir -Force
foreach ($name in @('ffmpeg.exe', 'ffprobe.exe')) {
    Copy-Item -LiteralPath (Join-Path $FFmpegDir $name) -Destination $stageDir -Force
}
foreach ($notice in @('ffmpeg-LICENSE.txt', 'ffmpeg-BUILD-README.txt', 'ffmpeg-SOURCE.txt')) {
    $noticePath = Join-Path $FFmpegDir $notice
    if (Test-Path -LiteralPath $noticePath) {
        Copy-Item -LiteralPath $noticePath -Destination $stageDir -Force
    }
}

Write-Step '部署 Qt 运行库'
& $windeployqt --release --no-translations --no-system-d3d-compiler `
    --no-opengl-sw --compiler-runtime (Join-Path $stageDir 'LiveMixStudio.exe')
if ($LASTEXITCODE -ne 0) {
    throw "windeployqt 部署失败（退出码 $LASTEXITCODE）。"
}

# The desktop app needs no input-context plugins; removing them also prevents
# accidentally shipping Qt Virtual Keyboard components.
$inputContexts = Join-Path $stageDir 'platforminputcontexts'
if (Test-Path -LiteralPath $inputContexts) {
    Remove-Item -LiteralPath $inputContexts -Recurse -Force
}

$multimediaPlugins = Join-Path $stageDir 'multimedia'
if (-not (Test-Path -LiteralPath $multimediaPlugins) -or
    @(Get-ChildItem -LiteralPath $multimediaPlugins -Filter '*.dll').Count -eq 0) {
    throw "部署结果缺少 multimedia 后端插件：$multimediaPlugins"
}

foreach ($required in @(
        'LiveMixStudio.exe',
        'ffmpeg.exe',
        'ffprobe.exe',
        'Qt6Core.dll',
        'Qt6Gui.dll',
        'Qt6Widgets.dll',
        'Qt6Svg.dll',
        'Qt6Sql.dll',
        'Qt6Network.dll',
        'Qt6Multimedia.dll',
        'Qt6MultimediaWidgets.dll',
        'platforms\qwindows.dll',
        'sqldrivers\qsqlite.dll')) {
    if (-not (Test-Path -LiteralPath (Join-Path $stageDir $required))) {
        throw "部署结果缺少 $required：$stageDir"
    }
}

if (-not (Test-Path -LiteralPath (Join-Path $stageDir 'vcruntime140.dll'))) {
    Write-Warning '未复制 vcruntime140.dll，目标机器可能需要安装 VC++ 运行库。'
}

$ffmpegVersion = Get-ToolVersion -Exe (Join-Path $stageDir 'ffmpeg.exe') -Label '安装包中的 ffmpeg.exe'

# --- build NSIS installer ---------------------------------------------------

$makensis = $null
$command = Get-Command makensis.exe -ErrorAction SilentlyContinue
if ($command) {
    $makensis = $command.Source
}
if (-not $makensis) {
    $nsisCandidates = @()
    if (${env:ProgramFiles(x86)}) {
        $nsisCandidates += Join-Path ${env:ProgramFiles(x86)} 'NSIS\makensis.exe'
    }
    if ($env:ProgramFiles) {
        $nsisCandidates += Join-Path $env:ProgramFiles 'NSIS\makensis.exe'
    }
    foreach ($candidate in $nsisCandidates) {
        if (Test-Path -LiteralPath $candidate) {
            $makensis = $candidate
            break
        }
    }
}
if (-not $makensis) {
    throw '未找到 makensis.exe。请先安装 NSIS（choco install nsis）。'
}

$versionFile = Join-Path $buildPath 'livemix-version.txt'
$builtVersion = ''
if (Test-Path -LiteralPath $versionFile) {
    $builtVersion = (Get-Content -LiteralPath $versionFile -Raw).Trim()
}

if ($Version) {
    if ($builtVersion -and $builtVersion -ne $Version) {
        throw "构建产物版本为 ${builtVersion}，与 -Version ${Version} 不一致。"
    }
    $version = $Version
} elseif ($builtVersion) {
    $version = $builtVersion
} else {
    Write-Warning "未找到构建版本信息（${versionFile}），回退到 1.0.0；建议重新编译。"
    $version = '1.0.0'
}
$versionParts = @($version.Split('.'))
while ($versionParts.Count -lt 4) {
    $versionParts += '0'
}
$viVersion = ($versionParts[0..3] -join '.')

New-Item -ItemType Directory -Path $outputPath -Force | Out-Null
$installerPath = Join-Path $outputPath "LiveMixStudio-$version-win64-setup.exe"
$nsiTemplate = Join-Path $PSScriptRoot 'LiveMixStudio.nsi'
if (-not (Test-Path -LiteralPath $nsiTemplate)) {
    throw "缺少 NSIS 模板：$nsiTemplate"
}

$definesFile = Join-Path $outputPath 'installer-build.nsi'
$defines = @"
!define APP_VERSION "$version"
!define VI_VERSION "$viVersion"
!define APP_SOURCE_DIR "$stageDir"
!define OUT_FILE "$installerPath"
!include "$nsiTemplate"
"@
Set-Content -LiteralPath $definesFile -Value $defines -Encoding UTF8

Write-Step '生成 NSIS 安装包'
& $makensis /V4 $definesFile
if ($LASTEXITCODE -ne 0) {
    throw "NSIS 打包失败（退出码 $LASTEXITCODE）。定义文件保留在 $definesFile"
}
Remove-Item -LiteralPath $definesFile -Force

if (-not (Test-Path -LiteralPath $installerPath)) {
    throw "安装包未生成：$installerPath"
}

$hash = (Get-FileHash -LiteralPath $installerPath -Algorithm SHA256).Hash.ToLowerInvariant()
$hashFile = "$installerPath.sha256"
"$hash  $(Split-Path -Leaf $installerPath)" |
    Set-Content -LiteralPath $hashFile -Encoding ASCII

$sizeMB = [math]::Round((Get-Item -LiteralPath $installerPath).Length / 1MB, 1)

Write-Step '打包完成'
Write-Host "  安装包: $installerPath"
Write-Host "  版本:   $version"
Write-Host "  大小:   $sizeMB MB"
Write-Host "  SHA256: $hash"
Write-Host "  FFmpeg: $ffmpegVersion"
