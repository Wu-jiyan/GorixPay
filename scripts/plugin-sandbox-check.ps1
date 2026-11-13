# GorixPay 通道（插件）联调检查脚本
#
# 用途：把每个已配置的支付渠道交给对应插件发起一次真实下单，用生产/沙箱凭据验证
#       网关连通性、签名是否正确、返回结构是否可解析。不写订单、不改状态。
#
# 前置：服务已启动；管理员已创建；要检查的渠道已在运营后台配置好真实凭据
#       （沙箱渠道不需要凭据，会返回本地 sandbox 支付链接）。
#
# 用法：
#   pwsh -File scripts/plugin-sandbox-check.ps1 -BaseUrl http://127.0.0.1:8080
#   pwsh -File scripts/plugin-sandbox-check.ps1 -SkipSandbox      # 只测真实网关
#   pwsh -File scripts/plugin-sandbox-check.ps1 -Channel sandbox-alipay
#
# 退出码：0 全部通过；1 有渠道失败（失败明细在输出里）。

[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://127.0.0.1:8080',
    [string]$AdminUser = 'admin',
    [string]$AdminPassword = 'admin123',
    [string]$Channel = '',
    [switch]$SkipSandbox,
    [int]$Amount = 100
)

$ErrorActionPreference = 'Stop'
$BaseUrl = $BaseUrl.TrimEnd('/')
$script:Failures = 0

function Write-Step([string]$Message) { Write-Host "`n=== $Message ===" -ForegroundColor Cyan }

function Write-Result([bool]$Ok, [string]$Name, [string]$Detail) {
    if ($Ok) {
        Write-Host ("  [PASS] {0}  {1}" -f $Name, $Detail) -ForegroundColor Green
    } else {
        $script:Failures++
        Write-Host ("  [FAIL] {0}  {1}" -f $Name, $Detail) -ForegroundColor Red
    }
}

function Get-Items($Response) {
    if ($null -eq $Response) { return @() }
    if ($Response.PSObject.Properties.Name -contains 'items') { return @($Response.items) }
    return @($Response)
}

function Invoke-Api {
    param(
        [string]$Method,
        [string]$Path,
        $Body = $null,
        [string]$Token = ''
    )
    $headers = @{ Accept = 'application/json' }
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }
    $params = @{
        Method      = $Method
        Uri         = "$BaseUrl$Path"
        Headers     = $headers
        TimeoutSec  = 30
        ErrorAction = 'Stop'
    }
    if ($null -ne $Body) {
        $params['ContentType'] = 'application/json'
        $params['Body'] = ($Body | ConvertTo-Json -Depth 6 -Compress)
    }
    return Invoke-RestMethod @params
}

Write-Step "管理员登录"
$login = Invoke-Api -Method Post -Path '/api/v1/auth/administrator/login' -Body @{
    identifier = $AdminUser
    password   = $AdminPassword
}
$token = $login.token
if (-not $token) { throw '管理员登录失败：响应里没有 token' }
Write-Host "  已登录：$AdminUser"

Write-Step "读取插件注册表"
$plugins = @(Get-Items (Invoke-Api -Method Get -Path '/api/v1/admin/plugins' -Token $token))
$pluginCodes = @($plugins | ForEach-Object { $_.code })
Write-Host ("  已注册插件：{0}" -f ($pluginCodes -join ', '))

Write-Step "读取支付渠道"
$channels = @(Get-Items (Invoke-Api -Method Get -Path '/api/v1/admin/channels' -Token $token))
if ($channels.Count -eq 0) {
    Write-Host '  没有配置任何渠道，先到运营后台「支付渠道」新建一个。' -ForegroundColor Yellow
    exit 0
}
if ($Channel) {
    $channels = @($channels | Where-Object { $_.name -eq $Channel -or $_.id -eq $Channel })
    if ($channels.Count -eq 0) { throw "找不到渠道：$Channel" }
}
Write-Host ("  待检查渠道：{0} 个" -f $channels.Count)

Write-Step "逐个渠道发起测试支付"
foreach ($item in $channels) {
    $name = if ($item.name) { $item.name } else { $item.id }
    $pluginCode = if ($item.plugin_code) { $item.plugin_code } else { $item.pluginCode }
    if ($SkipSandbox -and $pluginCode -eq 'sandbox') {
        Write-Host ("  [SKIP] {0}（沙箱渠道，按 -SkipSandbox 跳过）" -f $name) -ForegroundColor DarkGray
        continue
    }
    if ($pluginCodes -notcontains $pluginCode) {
        Write-Result $false $name "插件 $pluginCode 未注册"
        continue
    }
    $paymentType = if ($item.payment_type) { $item.payment_type } else { 'alipay' }
    try {
        $result = Invoke-Api -Method Post -Path "/api/v1/admin/channels/$($item.id)/test" -Token $token -Body @{
            payment_type = $paymentType
            amount       = $Amount
        }
        if ($result.ok) {
            $payType = $result.pay_type
            $payInfo = "$($result.pay_info)"
            if ($payInfo.Length -gt 90) { $payInfo = $payInfo.Substring(0, 90) + '...' }
            Write-Result $true $name ("plugin={0} pay_type={1} pay_info={2}" -f $pluginCode, $payType, $payInfo)
        } else {
            Write-Result $false $name ("plugin={0} error={1}" -f $pluginCode, $result.error)
        }
    } catch {
        $detail = $_.Exception.Message
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $detail = $_.ErrorDetails.Message }
        Write-Result $false $name ("请求异常：{0}" -f $detail)
    }
}

Write-Step "结果"
if ($script:Failures -eq 0) {
    Write-Host '  所有渠道均通过（真实网关已返回可解析的支付参数）。' -ForegroundColor Green
    exit 0
}
Write-Host ("  {0} 个渠道失败，检查渠道配置/凭据/回调白名单。" -f $script:Failures) -ForegroundColor Red
exit 1
