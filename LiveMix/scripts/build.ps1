<#
.SYNOPSIS
    LiveMix Studio 一键编译脚本（Windows）。

.DESCRIPTION
    自动查找 CMake 与常见位置的 Qt 6 安装，支持 Debug/Release、清理和编译后启动。
    推荐通过同目录下的 build.bat 调用，便于在资源管理器或普通 cmd 中双击/运行。

.PARAMETER Configuration
    Debug、Release 或 RelWithDebInfo，默认 Debug。

.PARAMETER Clean
    编译前删除构建目录。

.PARAMETER Run
    编译成功后启动程序。

.PARAMETER Jobs
    并行编译线程数，默认交给 CMake 决定。

.PARAMETER QtPath
    Qt 6 安装目录，例如 C:\Qt\6.12.0\msvc2022_64。省略时会尝试自动查找。

.PARAMETER FFmpegDir
    ffmpeg/ffprobe 所在目录。指定后会把两个可执行文件复制到应用输出目录。

.PARAMETER BuildDir
    构建目录，默认 build。

.PARAMETER ExtraCMakeArgs
    其余参数会原样传给 CMake configure。

.EXAMPLE
    .\scripts\build.ps1

.EXAMPLE
    .\scripts\build.ps1 -Configuration Release -Run

.EXAMPLE
    .\scripts\build.ps1 -Configuration Release -Clean -QtPath C:\Qt\6.12.0\msvc2022_64
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('Debug', 'Release', 'RelWithDebInfo')]
    [string]$Configuration = 'Debug',

    [switch]$Clean,
    [switch]$Run,

    [int]$Jobs = 0,
    [string]$QtPath = '',
    [string]$FFmpegDir = '',
    [string]$BuildDir = 'build',

    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$ExtraCMakeArgs = @()
)

$ErrorActionPreference = 'Stop'

# Normalize a lower-case command line such as `build.bat release`.
switch ($Configuration.ToLowerInvariant()) {
    'debug' { $Configuration = 'Debug' }
    'release' { $Configuration = 'Release' }
    'relwithdebinfo' { $Configuration = 'RelWithDebInfo' }
}

function Write-Step {
    param([string]$Message)
    Write-Host ''
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Find-CMake {
    $command = Get-Command cmake.exe -ErrorAction SilentlyContinue
    if ($null -ne $command) {
        return $command.Source
    }

    $candidates = @()
    if ($env:ProgramFiles) {
        $candidates += Join-Path $env:ProgramFiles 'CMake\bin\cmake.exe'
    }
    $programFilesX86 = [Environment]::GetEnvironmentVariable('ProgramFiles(x86)')
    if ($programFilesX86) {
        $candidates += Join-Path $programFilesX86 'CMake\bin\cmake.exe'
    }
    foreach ($candidate in $candidates) {
        if (Test-Path -LiteralPath $candidate) {
            return $candidate
        }
    }
    return $null
}

function Test-QtPrefix {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $false
    }
    return Test-Path -LiteralPath (Join-Path $Path 'lib\cmake\Qt6\Qt6Config.cmake')
}

function Find-QtPrefix {
    param([string]$ExplicitPath)

    if (-not [string]::IsNullOrWhiteSpace($ExplicitPath)) {
        if (-not (Test-QtPrefix $ExplicitPath)) {
            throw "指定的 Qt 路径无效（缺少 lib\cmake\Qt6\Qt6Config.cmake）：$ExplicitPath"
        }
        return $ExplicitPath
    }

    foreach ($candidate in @($env:QT_ROOT, $env:QTDIR)) {
        if (Test-QtPrefix $candidate) {
            return $candidate
        }
    }

    $roots = @(
        'C:\Qt',
        (Join-Path $env:USERPROFILE 'Qt')
    )
    $kits = @()
    foreach ($root in $roots) {
        if (-not (Test-Path -LiteralPath $root)) {
            continue
        }
        $kits += Get-ChildItem -LiteralPath $root -Directory -ErrorAction SilentlyContinue |
            Sort-Object Name -Descending |
            ForEach-Object {
                Get-ChildItem -LiteralPath $_.FullName -Directory -ErrorAction SilentlyContinue |
                    Where-Object { $_.Name -like 'msvc*_64' }
            }
    }
    foreach ($kit in ($kits | Sort-Object FullName -Descending)) {
        if (Test-QtPrefix $kit.FullName) {
            return $kit.FullName
        }
    }
    return $null
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
try {
    if ([string]::IsNullOrWhiteSpace($BuildDir)) {
        throw '构建目录不能为空。'
    }
    if ([System.IO.Path]::IsPathRooted($BuildDir)) {
        $buildPath = [System.IO.Path]::GetFullPath($BuildDir)
    } else {
        $buildPath = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $BuildDir))
    }

    $rootNormalized = $repoRoot.TrimEnd([char]'\')
    $buildNormalized = $buildPath.TrimEnd([char]'\')
    if ($Clean -and (
            $buildNormalized -eq $rootNormalized -or
            $buildPath -eq [System.IO.Path]::GetPathRoot($buildPath))) {
        throw "拒绝清理不安全的构建目录：$buildPath"
    }

    $cmake = Find-CMake
    if (-not $cmake) {
        throw '未找到 CMake。请安装 CMake 3.21+ 并加入 PATH。'
    }

    $qtPrefix = Find-QtPrefix $QtPath

    if (-not [string]::IsNullOrWhiteSpace($FFmpegDir)) {
        if ([System.IO.Path]::IsPathRooted($FFmpegDir)) {
            $FFmpegDir = [System.IO.Path]::GetFullPath($FFmpegDir)
        } else {
            $FFmpegDir = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $FFmpegDir))
        }
    }

    if ($Clean -and (Test-Path -LiteralPath $buildPath)) {
        $entries = @(Get-ChildItem -LiteralPath $buildPath -Force -ErrorAction SilentlyContinue)
        if ($entries.Count -gt 0 -and
            -not (Test-Path -LiteralPath (Join-Path $buildPath 'CMakeCache.txt'))) {
            throw "拒绝清理非 CMake 构建目录：${buildPath}（未找到 CMakeCache.txt）"
        }
        Write-Step "清理构建目录: $buildPath"
        Remove-Item -LiteralPath $buildPath -Recurse -Force
    }

    Write-Step '配置 LiveMix Studio'
    Write-Host "  源码目录: $repoRoot"
    Write-Host "  构建目录: $buildPath"
    Write-Host "  构建类型: $Configuration"
    if ($qtPrefix) {
        Write-Host "  Qt 前缀:  $qtPrefix"
    } else {
        Write-Host '  Qt 前缀:  由 CMake 自动查找（可使用 -QtPath 指定）'
    }
    if ($FFmpegDir) {
        Write-Host "  FFmpeg:   $FFmpegDir"
    }

    $configureArgs = @(
        '-S', $repoRoot,
        '-B', $buildPath,
        "-DCMAKE_BUILD_TYPE=$Configuration"
    )
    if ($qtPrefix) {
        $configureArgs += "-DCMAKE_PREFIX_PATH=$qtPrefix"
    }
    if ($FFmpegDir) {
        $configureArgs += "-DLIVEMIX_FFMPEG_DIR=$FFmpegDir"
    }
    if ($ExtraCMakeArgs.Count -gt 0) {
        $configureArgs += $ExtraCMakeArgs
    }

    & $cmake @configureArgs
    if ($LASTEXITCODE -ne 0) {
        throw "CMake 配置失败。请确认 Qt 6.12.0 已安装，必要时使用 -QtPath 指定 Qt 目录。"
    }

    Write-Step "编译 LiveMix Studio ($Configuration)"
    $buildArgs = @('--build', $buildPath, '--config', $Configuration)
    if ($Jobs -gt 0) {
        $buildArgs += @('--parallel', "$Jobs")
    } else {
        $buildArgs += '--parallel'
    }
    & $cmake @buildArgs
    if ($LASTEXITCODE -ne 0) {
        throw '编译失败。'
    }

    $exeCandidates = @(
        (Join-Path $buildPath "$Configuration\LiveMixStudio.exe"),
        (Join-Path $buildPath 'LiveMixStudio.exe')
    )
    $exePath = $null
    foreach ($candidate in $exeCandidates) {
        if (Test-Path -LiteralPath $candidate) {
            $exePath = $candidate
            break
        }
    }
    if (-not $exePath) {
        $found = Get-ChildItem -LiteralPath $buildPath -Filter 'LiveMixStudio.exe' -File -Recurse -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($found) {
            $exePath = $found.FullName
        }
    }

    Write-Step '编译完成'
    if ($exePath) {
        Write-Host "  可执行文件: $exePath"
    } else {
        Write-Warning '未在构建目录中找到 LiveMixStudio.exe。'
    }

    if ($Run) {
        if (-not $exePath) {
            throw '构建产物不存在，无法启动。'
        }
        Write-Step '启动 LiveMix Studio'
        Start-Process -FilePath $exePath | Out-Null
    }

    exit 0
}
catch {
    Write-Host ''
    Write-Host "错误: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
