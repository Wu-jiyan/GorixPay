# GorixPay 一键部署脚本（Windows，PowerShell 7+）
# 用法：pwsh -File scripts/deploy.ps1 [-DryRun]
# 与 scripts/deploy.sh 行为一致：检查 docker、准备 deploy\.env 与主密钥、compose up、轮询健康检查。
# 重复执行不会覆盖已存在的 deploy\.env 与 deploy\secrets\master_key.txt。

[CmdletBinding()]
param(
    [switch]$DryRun
)

if ($PSVersionTable.PSVersion.Major -lt 7) {
    Write-Host '需要 PowerShell 7（pwsh）：本脚本用 [IO.File] 写入无 BOM 文件并用 $IsWindows 判断平台，Windows PowerShell 5.1 跑不起来。' -ForegroundColor Red
    exit 2
}

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$DeployDir = Join-Path $Root 'deploy'
$EnvExample = Join-Path $DeployDir '.env.example'
$EnvFile = Join-Path $DeployDir '.env'
$SecretsDir = Join-Path $DeployDir 'secrets'
$MasterKeyFile = Join-Path $SecretsDir 'master_key.txt'
$ComposeFile = Join-Path $DeployDir 'compose.yaml'
$ComposeRel = 'deploy/compose.yaml'
$HealthUrl = 'http://127.0.0.1:8080/health/ready'
$HealthTimeoutSeconds = 180
$PlaceholderPublicUrl = 'https://pay.example.com'

function Write-Step([string]$Message) { Write-Host "`n== $Message ==" -ForegroundColor Cyan }

function Write-Info([string]$Message) { Write-Host "  $Message" }

function Write-Plan([string]$Message) { Write-Host "  [dry-run] $Message" -ForegroundColor DarkGray }

function New-RandomHex([int]$ByteCount) {
    $buffer = New-Object byte[] $ByteCount
    [Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
    return (-join ($buffer | ForEach-Object { $_.ToString('x2') }))
}

function New-RandomBase64([int]$ByteCount) {
    $buffer = New-Object byte[] $ByteCount
    [Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
    return [Convert]::ToBase64String($buffer)
}

function Get-EnvValue([string]$Path, [string]$Key) {
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line.StartsWith("$Key=")) {
            return $line.Substring($Key.Length + 1).TrimEnd("`r")
        }
    }
    return $null
}

function Set-EnvValue([string]$Path, [string]$Key, [string]$Value) {
    $lines = [IO.File]::ReadAllLines($Path)
    $result = foreach ($line in $lines) {
        if ($line.StartsWith("$Key=")) { "$Key=$Value" } else { $line }
    }
    [IO.File]::WriteAllText($Path, (($result -join "`n") + "`n"), (New-Object System.Text.UTF8Encoding($false)))
}

function Protect-Path([string]$Path) {
    if (-not $IsWindows) {
        return
    }
    try {
        & icacls $Path /inheritance:r /grant:r "${env:USERNAME}:(R,W)" *> $null
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  提示：icacls 收紧权限失败，请手动确认 $Path 只有你的账号可读。" -ForegroundColor Yellow
        }
    } catch {
        Write-Host "  提示：无法收紧 $Path 的权限，请手动确认只有你的账号可读。" -ForegroundColor Yellow
    }
}

function Write-SecretFile([string]$Path, [string]$Content) {
    [IO.File]::WriteAllText($Path, $Content, (New-Object System.Text.UTF8Encoding($false)))
    Protect-Path -Path $Path
}

Write-Step '检查 Docker'
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host @'
未找到 docker。安装方式：
  winget install --id Docker.DockerDesktop -e
  也可以从 https://docs.docker.com/desktop/setup/install/windows/ 下载安装包
装完后启动 Docker Desktop，等右下角鲸鱼图标变成 Running。
'@ -ForegroundColor Red
    exit 2
}
$null = & docker compose version 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host @'
未找到 docker compose 插件（v2）。Docker Desktop 自带这个插件，没有的话：
  winget install --id Docker.DockerDesktop -e
装完验证：docker compose version
'@ -ForegroundColor Red
    exit 2
}
Write-Info 'docker 与 docker compose 就绪'

Write-Step '准备 deploy\.env'
if (Test-Path -LiteralPath $EnvFile) {
    Write-Info 'deploy\.env 已存在，保持不变'
} elseif ($DryRun) {
    Write-Plan '复制 deploy\.env.example 到 deploy\.env'
    Write-Plan '写入随机 POSTGRES_PASSWORD（16 字节十六进制，不打印）'
    Write-Plan '提示修改 GORIXPAY_PUBLIC_URL 并退出 0'
} else {
    $generatedPassword = New-RandomHex 16
    Copy-Item -LiteralPath $EnvExample -Destination $EnvFile
    Set-EnvValue -Path $EnvFile -Key 'POSTGRES_PASSWORD' -Value $generatedPassword
    Protect-Path -Path $EnvFile
    Write-Info '已生成 deploy\.env 并写入随机 PostgreSQL 口令（不显示数值）'
    Write-Info '下一步：编辑 deploy\.env，把 GORIXPAY_PUBLIC_URL 改成真实地址，然后重新执行本脚本。'
    exit 0
}

Write-Step '检查对外地址'
$publicUrl = $null
if (Test-Path -LiteralPath $EnvFile) {
    $publicUrl = Get-EnvValue -Path $EnvFile -Key 'GORIXPAY_PUBLIC_URL'
}
if ([string]::IsNullOrWhiteSpace($publicUrl) -or $publicUrl -eq $PlaceholderPublicUrl) {
    if ($DryRun) {
        Write-Plan 'GORIXPAY_PUBLIC_URL 还是占位值，真实运行到这里会停下提示修改后重跑'
        $publicUrl = $PlaceholderPublicUrl
    } else {
        Write-Info "deploy\.env 里的 GORIXPAY_PUBLIC_URL 还是占位值（$PlaceholderPublicUrl）"
        Write-Info '改成真实地址后重新执行本脚本。'
        exit 0
    }
} else {
    Write-Info "对外地址：$publicUrl"
}

$postgresPassword = $null
if (Test-Path -LiteralPath $EnvFile) {
    $postgresPassword = Get-EnvValue -Path $EnvFile -Key 'POSTGRES_PASSWORD'
}
if (-not $DryRun -and [string]::IsNullOrWhiteSpace($postgresPassword)) {
    Write-Info 'deploy\.env 里的 POSTGRES_PASSWORD 为空，请填一个随机口令后重跑。'
    exit 2
}

Write-Step '准备 deploy\secrets'
if ($DryRun) {
    Write-Plan '创建目录 deploy\secrets（已存在则跳过）'
    Write-Plan '不存在时生成 deploy\secrets\master_key.txt：32 字节随机数的 base64，权限只给当前用户'
    Write-Plan '已存在时保持原样，不覆盖'
} else {
    if (-not (Test-Path -LiteralPath $SecretsDir)) {
        New-Item -ItemType Directory -Path $SecretsDir | Out-Null
    }
    if (Test-Path -LiteralPath $MasterKeyFile) {
        Write-Info 'deploy\secrets\master_key.txt 已存在，保持不变'
    } else {
        Write-SecretFile -Path $MasterKeyFile -Content ((New-RandomBase64 32) + "`n")
        Write-Info '已生成 deploy\secrets\master_key.txt（内容不显示，丢失后已加密字段无法解密）'
    }
}

Write-Step '启动容器'
if ($DryRun) {
    Write-Plan "docker compose -f $ComposeRel up -d --build"
} else {
    Write-Info "+ docker compose -f $ComposeRel up -d --build"
    & docker compose -f $ComposeFile up -d --build
    if ($LASTEXITCODE -ne 0) {
        Write-Host "docker compose up 失败，退出码 $LASTEXITCODE。" -ForegroundColor Red
        exit 1
    }
}

Write-Step '等待健康检查'
if ($DryRun) {
    Write-Plan "轮询 $HealthUrl，最多 $HealthTimeoutSeconds 秒，每 3 秒一次"
} else {
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSeconds)
    $healthy = $false
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-WebRequest -Uri $HealthUrl -TimeoutSec 3 -ErrorAction Stop
            if ($response.StatusCode -eq 200) {
                $healthy = $true
                break
            }
        } catch {
            Write-Host -NoNewline '.'
        }
        Start-Sleep -Seconds 3
    }
    Write-Host ''
    if (-not $healthy) {
        Write-Info "健康检查 $HealthTimeoutSeconds 秒内没有通过，下面是 gorixpay 最后 50 行日志："
        & docker compose -f $ComposeFile logs --tail 50 gorixpay
        exit 1
    }
    Write-Info '健康检查通过'
}

Write-Step '部署完成'
Write-Info "访问地址：$publicUrl"
Write-Info "运营后台：$publicUrl/admin/login"
Write-Info '数据卷：deploy_postgres_data、deploy_redis_data'
Write-Info "查看卷路径：docker volume inspect deploy_postgres_data --format '{{.Mountpoint}}'"
Write-Info "删除卷会连数据一起删掉：docker compose -f $ComposeRel down -v"
Write-Host @'

创建管理员（在仓库根目录执行，会交互式提示输入口令，至少 8 位）：
  docker compose -f deploy/compose.yaml exec -it gorixpay gorixpay admin create `
    --username admin --display-name 管理员

主密钥从容器内的 /run/secrets/master_key 读取，不需要再传 --master-key-file。
'@
