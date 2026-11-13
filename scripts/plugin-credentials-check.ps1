# GorixPay 通道凭据校验 / 下发脚本
#
# 作用：读你填好的凭据文件，按 /api/v1/admin/plugins 的 config_fields 校验字段
#       是否齐全、是否还是模板占位符；加 -Apply 时按 name 新建或更新渠道。
#       不打印任何 secret 值。
#
# 用法：
#   Copy-Item deploy/plugin-credentials.example.ps1 deploy/plugin-credentials.local.ps1
#   # 填值……
#   powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1
#   powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1 -Apply
#   powershell -File scripts/plugin-sandbox-check.ps1 -SkipSandbox      # 真正打网关
#
# 退出码：0 全部就绪；1 有渠道缺字段/仍是占位符。

[CmdletBinding()]
param(
    [string]$File = 'deploy/plugin-credentials.local.ps1',
    [string]$BaseUrl = 'http://127.0.0.1:8080',
    [string]$AdminUser = 'admin',
    [string]$AdminPassword = 'admin123',
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'
$BaseUrl = $BaseUrl.TrimEnd('/')
$script:Failures = 0

if (-not (Test-Path $File)) {
    Write-Host "找不到凭据文件：$File" -ForegroundColor Red
    Write-Host "先复制模板：Copy-Item deploy/plugin-credentials.example.ps1 deploy/plugin-credentials.local.ps1" -ForegroundColor Yellow
    exit 2
}

$Channels = @()
. (Resolve-Path $File)
$Channels = @($Channels)
if ($Channels.Count -eq 0) {
    Write-Host '凭据文件里没有任何渠道定义（$Channels 为空）。' -ForegroundColor Yellow
    exit 2
}

function Invoke-Api {
    param([string]$Method, [string]$Path, $Body = $null, [string]$Token = '')
    $headers = @{ Accept = 'application/json' }
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }
    $params = @{ Method = $Method; Uri = "$BaseUrl$Path"; Headers = $headers; TimeoutSec = 30; ErrorAction = 'Stop' }
    if ($null -ne $Body) {
        $params['ContentType'] = 'application/json'
        $params['Body'] = ($Body | ConvertTo-Json -Depth 8 -Compress)
    }
    return Invoke-RestMethod @params
}

function Get-Items($Response) {
    if ($null -eq $Response) { return @() }
    if ($Response.PSObject.Properties.Name -contains 'items') { return @($Response.items) }
    return @($Response)
}

function Test-Placeholder([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    if ($Value -match '^\.{2,}') { return $true }
    if ($Value -match '填') { return $true }
    if ($Value -match '^<\w+>$') { return $true }
    if ($Value -eq '32字节APIv3密钥') { return $true }
    return $false
}

Write-Host "`n=== 管理员登录 ===" -ForegroundColor Cyan
$login = Invoke-Api -Method Post -Path '/api/v1/auth/administrator/login' -Body @{ identifier = $AdminUser; password = $AdminPassword }
if (-not $login.token) { throw '管理员登录失败：响应里没有 token' }
$token = $login.token
Write-Host "  已登录：$AdminUser"

Write-Host "`n=== 读取插件配置字段 ===" -ForegroundColor Cyan
$schemas = @{}
foreach ($plugin in (Get-Items (Invoke-Api -Method Get -Path '/api/v1/admin/plugins' -Token $token))) {
    $schemas[$plugin.code] = $plugin
}
Write-Host ("  已注册插件：{0}" -f (($schemas.Keys | Sort-Object) -join ', '))

$existing = @(Get-Items (Invoke-Api -Method Get -Path '/api/v1/admin/channels' -Token $token))

Write-Host "`n=== 校验凭据 ===" -ForegroundColor Cyan
$ready = @()
foreach ($entry in $Channels) {
    $name = $entry['name']
    $pluginCode = $entry['plugin_code']
    $issues = @()

    if ([string]::IsNullOrWhiteSpace($name)) { $issues += '缺少 name' }
    if ([string]::IsNullOrWhiteSpace($pluginCode)) { $issues += '缺少 plugin_code' }
    if ([string]::IsNullOrWhiteSpace($entry['payment_type'])) { $issues += '缺少 payment_type' }

    $schema = $schemas[$pluginCode]
    if ($null -eq $schema) {
        $issues += "插件未注册：$pluginCode"
    } else {
        $config = $entry['config']
        if ($null -eq $config) { $config = @{} }
        foreach ($field in $schema.config_fields) {
            $value = "$($config[$field.key])"
            if ($field.required -and [string]::IsNullOrWhiteSpace($value)) {
                $issues += "缺少必填 $($field.key)（$($field.label)）"
            } elseif (Test-Placeholder $value) {
                $issues += "$($field.key) 还是占位符"
            }
        }
    }

    if ($pluginCode -eq 'epay') {
        foreach ($legacy in @('api_url', 'pid', 'key')) {
            if ([string]::IsNullOrWhiteSpace("$($entry[$legacy])")) { $issues += "epay 缺少 $legacy" }
        }
    }

    if ($issues.Count -eq 0) {
        Write-Host ("  [OK]   {0}  plugin={1}" -f $name, $pluginCode) -ForegroundColor Green
        $ready += $entry
    } else {
        $script:Failures++
        Write-Host ("  [FAIL] {0}  plugin={1}" -f $name, $pluginCode) -ForegroundColor Red
        foreach ($issue in $issues) { Write-Host ("         - {0}" -f $issue) -ForegroundColor DarkYellow }
    }
}

if ($Apply -and $ready.Count -gt 0) {
    Write-Host "`n=== 写入渠道（-Apply） ===" -ForegroundColor Cyan
    foreach ($entry in $ready) {
        $name = $entry['name']
        $priority = 0
        if ($null -ne $entry['priority']) { $priority = [int]$entry['priority'] }
        $weight = 1
        if ($null -ne $entry['weight']) { $weight = [int]$entry['weight'] }
        $status = 'active'
        if ($entry['status']) { $status = "$($entry['status'])" }
        $config = @{}
        if ($entry['config']) { $config = $entry['config'] }
        $body = @{
            name         = $name
            plugin_code  = $entry['plugin_code']
            payment_type = $entry['payment_type']
            priority     = $priority
            weight       = $weight
            status       = $status
            api_url      = "$($entry['api_url'])"
            pid          = "$($entry['pid'])"
            key          = "$($entry['key'])"
            config       = $config
        }
        $current = @($existing | Where-Object { $_.name -eq $name })
        try {
            if ($current.Count -gt 0) {
                Invoke-Api -Method Put -Path "/api/v1/admin/channels/$($current[0].id)" -Token $token -Body $body | Out-Null
                Write-Host ("  [UPDATED] {0}" -f $name) -ForegroundColor Green
            } else {
                $created = Invoke-Api -Method Post -Path '/api/v1/admin/channels' -Token $token -Body $body
                Write-Host ("  [CREATED] {0} ({1})" -f $name, $created.id) -ForegroundColor Green
            }
        } catch {
            $detail = $_.Exception.Message
            if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $detail = $_.ErrorDetails.Message }
            $script:Failures++
            Write-Host ("  [ERROR]   {0}  {1}" -f $name, $detail) -ForegroundColor Red
        }
    }
} elseif (-not $Apply) {
    Write-Host "`n（只校验未写入；确认无误后加 -Apply 下发到数据库）" -ForegroundColor DarkGray
}

Write-Host "`n=== 结果 ===" -ForegroundColor Cyan
if ($script:Failures -eq 0) {
    Write-Host '  全部就绪。下一步：powershell -File scripts/plugin-sandbox-check.ps1 -SkipSandbox' -ForegroundColor Green
    exit 0
}
Write-Host ("  {0} 个渠道需要补全，见上面的 - 列表。" -f $script:Failures) -ForegroundColor Red
exit 1
