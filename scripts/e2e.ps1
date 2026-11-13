# GorixPay 端到端验收脚本（PowerShell 7+）
# 前置：服务已启动（默认 http://127.0.0.1:8080），管理员已创建（默认 admin/admin123），
#       运行在 development（沙箱渠道启用）。
# 用法：pwsh -File scripts/e2e.ps1 [-BaseUrl http://127.0.0.1:8080] [-AdminUser admin] [-AdminPassword admin123]

[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://127.0.0.1:8080',
    [string]$AdminUser = 'admin',
    [string]$AdminPassword = 'admin123'
)

if ($PSVersionTable.PSVersion.Major -lt 7) {
    Write-Host '需要 PowerShell 7（pwsh）：本脚本用 .NET 的 RSA.ImportFromPem 处理 V2 密钥，Windows PowerShell 5.1 没有这个方法。' -ForegroundColor Red
    exit 2
}

$ErrorActionPreference = 'Stop'
$script:Failures = 0
$BaseUrl = $BaseUrl.TrimEnd('/')

function Write-Step([string]$Message) { Write-Host "`n=== $Message ===" -ForegroundColor Cyan }

function Assert-True([bool]$Condition, [string]$Message) {
    if ($Condition) { Write-Host "  [PASS] $Message" -ForegroundColor Green }
    else { $script:Failures++; Write-Host "  [FAIL] $Message" -ForegroundColor Red }
}

function Assert-Equal($Actual, $Expected, [string]$Message) {
    if ("$Actual" -eq "$Expected") { Write-Host "  [PASS] $Message ($Actual)" -ForegroundColor Green }
    else { $script:Failures++; Write-Host "  [FAIL] $Message -> expected '$Expected', got '$Actual'" -ForegroundColor Red }
}

function Get-UnixTimestamp { return [string][DateTimeOffset]::UtcNow.ToUnixTimeSeconds() }

function Get-Canonical([hashtable]$Parameters) {
    $keys = @($Parameters.Keys | Where-Object {
            $_ -ne 'sign' -and $_ -ne 'sign_type' -and $null -ne $Parameters[$_] -and "$($Parameters[$_])" -ne ''
        })
    [Array]::Sort($keys, [System.StringComparer]::Ordinal)
    return (($keys | ForEach-Object { "$_=$($Parameters[$_])" }) -join '&')
}

function Get-MD5Sign([hashtable]$Parameters, [string]$Key) {
    $canonical = Get-Canonical $Parameters
    $md5 = [System.Security.Cryptography.MD5]::Create()
    try {
        $hash = $md5.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($canonical + $Key))
        return (($hash | ForEach-Object { $_.ToString('x2') }) -join '')
    }
    finally { $md5.Dispose() }
}

function Get-RSASign([hashtable]$Parameters, [string]$PrivateKeyPem) {
    $canonical = Get-Canonical $Parameters
    $rsa = [System.Security.Cryptography.RSA]::Create()
    try {
        $rsa.ImportFromPem($PrivateKeyPem)
        $signature = $rsa.SignData(
            [System.Text.Encoding]::UTF8.GetBytes($canonical),
            [System.Security.Cryptography.HashAlgorithmName]::SHA256,
            [System.Security.Cryptography.RSASignaturePadding]::Pkcs1)
        return [Convert]::ToBase64String($signature)
    }
    finally { $rsa.Dispose() }
}

function Test-RSASign([hashtable]$Parameters, [string]$PublicKeyPem, [string]$Signature) {
    $canonical = Get-Canonical $Parameters
    $rsa = [System.Security.Cryptography.RSA]::Create()
    try {
        $rsa.ImportFromPem($PublicKeyPem)
        return $rsa.VerifyData(
            [System.Text.Encoding]::UTF8.GetBytes($canonical),
            [Convert]::FromBase64String($Signature),
            [System.Security.Cryptography.HashAlgorithmName]::SHA256,
            [System.Security.Cryptography.RSASignaturePadding]::Pkcs1)
    }
    finally { $rsa.Dispose() }
}

function ConvertTo-Hashtable($Object) {
    $table = @{}
    if ($null -eq $Object) { return $table }
    $Object.PSObject.Properties | ForEach-Object { $table[$_.Name] = $_.Value }
    return $table
}

function Invoke-Form([string]$Path, [hashtable]$Body, [string]$Token = '') {
    $headers = @{}
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }
    $pairs = @()
    foreach ($key in $Body.Keys) {
        if ($null -eq $Body[$key]) { continue }
        $pairs += ("{0}={1}" -f [Uri]::EscapeDataString($key), [Uri]::EscapeDataString([string]$Body[$key]))
    }
    return Invoke-RestMethod -Method Post -Uri "$BaseUrl$Path" -Headers $headers `
        -ContentType 'application/x-www-form-urlencoded' -Body ($pairs -join '&') -TimeoutSec 30
}

function Invoke-Json([string]$Method, [string]$Path, $Body, [string]$Token = '') {
    $headers = @{}
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }
    $parameters = @{ Method = $Method; Uri = "$BaseUrl$Path"; Headers = $headers; TimeoutSec = 30 }
    if ($null -ne $Body) {
        $parameters['ContentType'] = 'application/json'
        $parameters['Body'] = ($Body | ConvertTo-Json -Depth 8 -Compress)
    }
    return Invoke-RestMethod @parameters
}

function New-PaidOrder([string]$Amount, [string]$Tag) {
    $outTradeNo = "E2E$Tag$script:Suffix"
    $parameters = @{
        pid          = "$script:MerchantPid"
        type         = 'alipay'
        out_trade_no = $outTradeNo
        notify_url   = "$BaseUrl/notify/$Tag"
        return_url   = "$BaseUrl/"
        name         = "E2E 测试商品 $Tag"
        money        = $Amount
        clientip     = '127.0.0.1'
        sign_type    = 'MD5'
    }
    $parameters['sign'] = Get-MD5Sign $parameters $script:MerchantKey
    $order = Invoke-Form '/mapi.php' $parameters
    if ($order.code -ne 1) { throw "下单失败：$($order.msg)" }
    $start = Invoke-Json Post "/api/v1/cashier/orders/$($order.trade_no)/start" $null
    if (-not $start.pay_type) { throw "收银台发起失败：$($start | ConvertTo-Json -Compress)" }
    $complete = Invoke-Json Post "/api/v1/sandbox/orders/$($order.trade_no)/complete" $null
    if ($complete.status -ne 'paid') { throw "沙箱支付失败：$($complete | ConvertTo-Json -Compress)" }
    return [pscustomobject]@{ TradeNo = $order.trade_no; OutTradeNo = $outTradeNo }
}

try {
    Write-Step '0. 健康检查'
    $live = Invoke-RestMethod -Method Get -Uri "$BaseUrl/health/live"
    Assert-Equal $live.status 'ok' 'health/live'

    Write-Step '1. 管理员登录'
    $adminLogin = Invoke-Json Post '/api/v1/auth/administrator/login' @{ identifier = $AdminUser; password = $AdminPassword }
    $adminToken = $adminLogin.token
    Assert-True ([bool]$adminToken) '管理员 token 获取成功'

    Write-Step '2. 创建/复用 sandbox 渠道'
    $channels = (Invoke-Json Get '/api/v1/admin/channels' $null $adminToken).items
    $sandboxChannel = $channels | Where-Object { $_.plugin_code -eq 'sandbox' } | Select-Object -First 1
    if (-not $sandboxChannel) {
        $created = Invoke-Json Post '/api/v1/admin/channels' @{
            name         = '沙箱支付'
            plugin_code  = 'sandbox'
            payment_type = 'alipay'
            priority     = 1
            weight       = 1
            status       = 'active'
            api_url      = 'http://sandbox.local'
            pid          = '0'
            key          = 'sandbox'
        } $adminToken
        $sandboxChannel = @{ id = $created.id; plugin_code = 'sandbox' }
    }
    Assert-True ([bool]$sandboxChannel.id) "sandbox 渠道存在：$($sandboxChannel.id)"

    Write-Step '3. 注册商户并轮换 MD5 密钥'
    $script:Suffix = (Get-Date -Format 'HHmmss') + (Get-Random -Minimum 100 -Maximum 999)
    $identifier = "e2e$($script:Suffix)@example.com"
    $register = Invoke-Json Post '/api/v1/auth/register' @{ identifier = $identifier; password = 'e2e-pass-123' }
    $merchantToken = $register.token
    $script:MerchantPid = $register.merchant.legacy_uid
    Assert-True ([bool]$script:MerchantPid) "商户 pid（legacy_uid）：$($script:MerchantPid)"
    $rotate = Invoke-Json Post '/api/v1/merchant/credentials/md5/rotate' $null $merchantToken
    $script:MerchantKey = $rotate.secret
    Assert-True ([bool]$script:MerchantKey) 'MD5 密钥轮换成功'

    Write-Step '4. 零元购防护：金额 0 / 负数 / 三位小数 / 科学计数法'
    foreach ($bad in @('0', '0.00', '-1', '0.001', '1e3')) {
        $badParams = @{
            pid = "$($script:MerchantPid)"; type = 'alipay'; out_trade_no = "BAD$($script:Suffix)$($bad -replace '[^0-9a-zA-Z]', '')"
            notify_url = "$BaseUrl/notify"; name = 'bad'; money = $bad; clientip = '127.0.0.1'; sign_type = 'MD5'
        }
        $badParams['sign'] = Get-MD5Sign $badParams $script:MerchantKey
        $badResponse = Invoke-Form '/mapi.php' $badParams
        Assert-Equal $badResponse.code -1 "金额 $bad 被拒绝"
    }

    Write-Step '5. V1 下单 + 收银台发起 + 沙箱支付'
    $order1 = New-PaidOrder '0.01' 'A'
    Assert-True ([bool]$order1.TradeNo) "订单 $($order1.TradeNo) 已支付"
    $status = Invoke-Json Get "/api/v1/cashier/orders/$($order1.TradeNo)/status" $null
    Assert-Equal $status.status 'paid' '状态轮询为 paid'

    Write-Step '6. 商户订单查询与仪表盘'
    $merchantOrders = Invoke-Json Get "/api/v1/merchant/orders?keyword=$($order1.TradeNo)" $null $merchantToken
    Assert-Equal $merchantOrders.items[0].status 'paid' '商户端订单状态'
    $merchantDashboard = Invoke-Json Get '/api/v1/merchant/dashboard' $null $merchantToken
    Assert-True ($merchantDashboard.today_paid_amount -ge 1) "今日收款（分）：$($merchantDashboard.today_paid_amount)"
    $outboxStats = Invoke-Json Get '/api/v1/admin/outbox/stats' $null $adminToken
    Assert-True ($null -ne $outboxStats) 'Outbox 统计可读取'

    Write-Step '7. V2：轮换 RSA 密钥并签名查询'
    $v2Keys = Invoke-Json Post '/api/v1/merchant/credentials/v2/rotate' $null $merchantToken
    Assert-True ([bool]$v2Keys.private_key) 'V2 商户私钥已生成（仅一次）'
    $platformPublicKey = $v2Keys.platform_public_key
    $queryParams = @{
        pid = "$($script:MerchantPid)"; out_trade_no = $order1.OutTradeNo; timestamp = (Get-UnixTimestamp); sign_type = 'RSA'
    }
    $queryParams['sign'] = Get-RSASign $queryParams $v2Keys.private_key
    $v2Query = Invoke-Form '/api/pay/query' $queryParams
    Assert-Equal $v2Query.code 0 'V2 查询成功'
    Assert-Equal $v2Query.trade_no $order1.TradeNo 'V2 返回订单号一致'
    Assert-Equal $v2Query.status 1 'V2 数字状态=1（已支付）'
    $verifyResponse = ConvertTo-Hashtable $v2Query
    $signature = $verifyResponse['sign']
    $verifyResponse.Remove('sign')
    $verifyResponse.Remove('sign_type')
    Assert-True (Test-RSASign $verifyResponse $platformPublicKey $signature) '平台响应 RSA 签名可验证'

    Write-Step '8. V2 安全：过期时间戳 / 篡改金额 / 重放'
    $expired = @{ pid = "$($script:MerchantPid)"; out_trade_no = $order1.OutTradeNo; timestamp = '1000000000'; sign_type = 'RSA' }
    $expired['sign'] = Get-RSASign $expired $v2Keys.private_key
    $expiredResponse = Invoke-Form '/api/pay/query' $expired
    Assert-Equal $expiredResponse.code -3 '过期 timestamp 被拒绝'

    $tampered = @{
        pid = "$($script:MerchantPid)"; out_trade_no = $order1.OutTradeNo; money = '0.00'
        timestamp = (Get-UnixTimestamp); sign_type = 'RSA'
    }
    $tampered['sign'] = Get-RSASign $tampered $v2Keys.private_key
    $tampered['money'] = '0.01'
    $tamperedResponse = Invoke-Form '/api/pay/query' $tampered
    Assert-Equal $tamperedResponse.code -3 '篡改金额后签名失效'

    Write-Step '9. V2 退款 + 重放拒绝'
    $refundParams = @{
        pid = "$($script:MerchantPid)"; trade_no = $order1.TradeNo; money = '0.01'
        out_refund_no = "E2ERF$($script:Suffix)"
        timestamp = (Get-UnixTimestamp); sign_type = 'RSA'
    }
    $refundParams['sign'] = Get-RSASign $refundParams $v2Keys.private_key
    $refundResponse = Invoke-Form '/api/pay/refund' $refundParams
    Assert-Equal $refundResponse.code 0 'V2 退款申请成功'
    $replayResponse = Invoke-Form '/api/pay/refund' $refundParams
    Assert-Equal $replayResponse.code -3 '重放退款请求被拒绝'

    Write-Step '10. V1 api.php 账户/订单查询 + 退款'
    $accountQuery = Invoke-Form '/api.php?act=query' @{ pid = "$($script:MerchantPid)"; key = $script:MerchantKey }
    Assert-Equal $accountQuery.code 1 'V1 act=query 成功'
    $orderQuery = Invoke-Form '/api.php?act=order' @{ pid = "$($script:MerchantPid)"; key = $script:MerchantKey; trade_no = $order1.TradeNo }
    Assert-Equal $orderQuery.code 1 'V1 act=order 成功'
    Assert-Equal $orderQuery.trade_no $order1.TradeNo 'V1 订单号一致'
    $order2 = New-PaidOrder '0.02' 'B'
    $v1Refund = Invoke-Form '/api.php?act=refund' @{ pid = "$($script:MerchantPid)"; key = $script:MerchantKey; trade_no = $order2.TradeNo; money = '0.02' }
    Assert-Equal $v1Refund.code 0 'V1 act=refund 成功'

    Write-Step '11. 伪造渠道回调被拒绝'
    try {
        $null = Invoke-Form "/api/v1/payment/callback/epay/$($sandboxChannel.id)" @{
            pid = "$($script:MerchantPid)"; trade_status = 'TRADE_SUCCESS'; out_trade_no = $order1.TradeNo; trade_no = 'FORGED'; money = '0.01'; sign = 'deadbeef'
        }
        Assert-True $false '伪造回调应返回非 2xx（实际 2xx）'
    }
    catch {
        $statusCode = $_.Exception.Response.StatusCode.value__
        Assert-True ($statusCode -ge 400) "伪造回调被拒绝（HTTP $statusCode）"
    }
}
catch {
    $script:Failures++
    Write-Host "`n脚本异常：$($_.Exception.Message)" -ForegroundColor Red
    Write-Host ($_.ScriptStackTrace) -ForegroundColor DarkGray
}

Write-Step '结果'
if ($script:Failures -eq 0) {
    Write-Host '全部通过 ✅' -ForegroundColor Green
    exit 0
}
Write-Host "失败 $($script:Failures) 项 ❌" -ForegroundColor Red
exit 1
