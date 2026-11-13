# GorixPay 通道凭据模板（复制成 plugin-credentials.local.ps1 再填）
#
# 用法：
#   1. Copy-Item deploy/plugin-credentials.example.ps1 deploy/plugin-credentials.local.ps1
#   2. 填下面每个 @{} 里的值（不需要的渠道整段删掉）
#   3. powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1
#      只校验字段是否齐、是否还留着占位符
#   4. 加 -Apply 把渠道写进数据库（按 name 匹配：存在则更新，不存在则新建）
#   5. powershell -File scripts/plugin-sandbox-check.ps1 -SkipSandbox
#      真正让插件拿这些凭据去网关下单
#
# 注意：
#   - 这个文件含私钥，别提交到 git；仓库 .gitignore 已忽略 *.local.ps1
#   - secret=true 的字段在后台接口里只写不读，脚本也不会把它打印出来
#   - 沙箱/正式地址不同，先填沙箱地址跑通再换正式

$Channels = @(

    # ---------- 支付宝当面付 ----------
    # 开放平台 → 应用 → 开发设置：
    #   app_id: 应用 APPID
    #   private_key: 应用私钥（PKCS8 PEM，含 -----BEGIN PRIVATE KEY-----）
    #   alipay_public_key: 支付宝公钥（不是应用公钥）
    @{
        name         = 'alipay-prod'
        plugin_code  = 'alipay'
        payment_type = 'alipay'
        priority     = 100
        weight       = 1
        status       = 'active'
        config       = @{
            app_id            = '2021000000000000'
            private_key       = '-----BEGIN PRIVATE KEY-----
...填应用私钥...
-----END PRIVATE KEY-----'
            alipay_public_key = '-----BEGIN PUBLIC KEY-----
...填支付宝公钥...
-----END PUBLIC KEY-----'
            gateway_url       = 'https://openapi.alipay.com/gateway.do'
        }
    }

    # ---------- 微信支付 V3 ----------
    # 商户平台 → 账户中心 → API安全：
    #   mch_id: 商户号
    #   serial_no: 商户 API 证书序列号
    #   private_key: 商户 API 私钥（apiclient_key.pem 内容）
    #   api_v3_key: APIv3 密钥（32 字节）
    #   platform_public_key / platform_serial: 平台证书公钥与序列号（可留空走自动下载）
    @{
        name         = 'wxpay-prod'
        plugin_code  = 'wechatpayv3'
        payment_type = 'wxpay'
        priority     = 90
        weight       = 1
        status       = 'active'
        config       = @{
            mch_id              = '1900000000'
            app_id              = 'wx0000000000000000'
            serial_no           = '4A00000000000000000000000000000000000000'
            private_key         = '-----BEGIN PRIVATE KEY-----
...填商户 API 私钥...
-----END PRIVATE KEY-----'
            api_v3_key          = '32字节APIv3密钥'
            platform_public_key = ''
            platform_serial     = ''
            api_base_url        = 'https://api.mch.weixin.qq.com'
        }
    }

    # ---------- QQ 钱包 ----------
    # QQ 钱包商户平台 → 密钥管理：mch_id / app_id / key（MD5 密钥）
    @{
        name         = 'qqpay-prod'
        plugin_code  = 'qqpay'
        payment_type = 'qqpay'
        priority     = 80
        weight       = 1
        status       = 'active'
        config       = @{
            mch_id  = '1000000000'
            app_id  = '1000000000'
            key     = '填QQ钱包商户密钥'
            api_url = 'https://qpay.qq.com/cgi-bin/pay/qpay_unified_order.cgi'
        }
    }

    # ---------- 银联全渠道 ----------
    # 银联商户入网后：mer_id（商户号）、cert_id（签名证书 ID）、
    #   商户私钥（PKCS8）、银联公钥；submit_mode 选 html（自动提交表单）或 jump
    @{
        name         = 'unionpay-prod'
        plugin_code  = 'unionpay'
        payment_type = 'unionpay'
        priority     = 70
        weight       = 1
        status       = 'active'
        config       = @{
            mer_id              = '777290058110048'
            cert_id             = '68759585097'
            private_key         = '-----BEGIN PRIVATE KEY-----
...填商户私钥...
-----END PRIVATE KEY-----'
            unionpay_public_key = '-----BEGIN PUBLIC KEY-----
...填银联公钥...
-----END PUBLIC KEY-----'
            api_url             = 'https://gateway.95516.com/gateway/api/frontTransReq.do'
            front_url           = 'http://127.0.0.1:8080/pay/ok/'
            submit_mode         = 'html'
        }
    }

    # ---------- Jeepay 聚合 ----------
    @{
        name         = 'jeepay-prod'
        plugin_code  = 'jeepay'
        payment_type = 'wxpay'
        priority     = 60
        weight       = 1
        status       = 'active'
        config       = @{
            api_url  = 'https://pay.example.com/api'
            mch_no   = 'M0000001'
            app_id   = '60d0f0f0f0f0f0f0f0f0f0f0'
            key      = '填Jeepay接口密钥'
            way_code = 'WX_NATIVE'
        }
    }

    # ---------- USDT（Epusdt 风格） ----------
    @{
        name         = 'usdt-prod'
        plugin_code  = 'usdt'
        payment_type = 'usdt'
        priority     = 50
        weight       = 1
        status       = 'active'
        config       = @{
            api_url    = 'https://epusdt.example.com/api/v1/order/create-transaction'
            key        = '填Epusdt商户密钥'
            action     = 'qrcode'
            notify_url = ''
        }
    }

    # ---------- 通用 HTTP 上游（长尾聚合用它接） ----------
    #   param_template 支持占位符：{trade_no} {amount} {name} {notify_url} {return_url} {clientip}
    @{
        name         = 'generic-prod'
        plugin_code  = 'generic'
        payment_type = 'bank'
        priority     = 10
        weight       = 1
        status       = 'active'
        config       = @{
            api_url        = 'https://upstream.example.com/pay/create'
            method         = 'POST'
            param_template = '{"mchid":"M1","out_trade_no":"{trade_no}","total_fee":{amount},"subject":"{name}","notify_url":"{notify_url}"}'
            sign_type      = 'md5'
            key            = '填上游密钥'
        }
    }

    # ---------- Epay 上游（用平台自带的 epay 插件对接另一个易支付） ----------
    @{
        name         = 'epay-upstream'
        plugin_code  = 'epay'
        payment_type = 'alipay'
        priority     = 5
        weight       = 1
        status       = 'active'
        api_url      = 'https://other-epay.example.com/'
        pid          = '1001'
        key          = '填上游商户密钥'
        config       = @{}
    }
)
