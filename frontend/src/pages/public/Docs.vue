<script setup lang="ts">
import PublicNav from '@/components/PublicNav.vue';

interface ParamRow {
  name: string;
  type: string;
  required: string;
  desc: string;
}

interface ParamGroup {
  title: string;
  summary: string;
  rows: ParamRow[];
}

const v1SubmitParams: ParamRow[] = [
  { name: 'pid', type: 'int', required: '是', desc: '商户 ID（商户号）' },
  { name: 'type', type: 'string', required: '否', desc: '支付类型：alipay / wxpay / qqpay / bank，留空进入收银台选择' },
  { name: 'out_trade_no', type: 'string', required: '是', desc: '商户订单号，同一商户下唯一，重复提交按幂等处理' },
  { name: 'notify_url', type: 'string', required: '是', desc: '异步通知地址，必须可公网访问，支持 http/https' },
  { name: 'return_url', type: 'string', required: '否', desc: '支付完成后的同步跳转地址' },
  { name: 'name', type: 'string', required: '是', desc: '商品名称，展示在收银台' },
  { name: 'money', type: 'string', required: '是', desc: '订单金额（元），最多两位小数，范围 0.01 ~ 100000.00' },
  { name: 'sitename', type: 'string', required: '否', desc: '网站名称，展示在收银台' },
  { name: 'param', type: 'string', required: '否', desc: '商户自定义参数，原样在通知中回传' },
  { name: 'clientip', type: 'string', required: '否', desc: '用户客户端 IP' },
  { name: 'sign', type: 'string', required: '是', desc: '签名，见「签名规则」' },
  { name: 'sign_type', type: 'string', required: '否', desc: 'MD5（缺省）或 RSA' },
];

const v1MapiParams: ParamRow[] = [
  { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
  { name: 'type', type: 'string', required: '是', desc: '支付类型：alipay / wxpay / qqpay / bank' },
  { name: 'out_trade_no', type: 'string', required: '是', desc: '商户订单号' },
  { name: 'notify_url', type: 'string', required: '是', desc: '异步通知地址' },
  { name: 'return_url', type: 'string', required: '否', desc: '同步跳转地址（部分类型需要）' },
  { name: 'name', type: 'string', required: '是', desc: '商品名称' },
  { name: 'money', type: 'string', required: '是', desc: '订单金额（元）' },
  { name: 'clientip', type: 'string', required: '否', desc: '用户客户端 IP' },
  { name: 'device', type: 'string', required: '否', desc: '设备类型：pc / mobile / qq / wechat 等' },
  { name: 'param', type: 'string', required: '否', desc: '自定义参数' },
  { name: 'sign', type: 'string', required: '是', desc: '签名' },
  { name: 'sign_type', type: 'string', required: '否', desc: 'MD5（缺省）或 RSA' },
];

const v1ApiActs = [
  {
    act: 'query',
    title: '账户信息',
    params: 'pid、key（明文密钥，官方 V1 语义）',
    returns: 'code=1，pid、money（可用余额，元）、orders（累计订单）、orders_today、orders_lastday、active',
  },
  {
    act: 'order',
    title: '订单查询',
    params: 'pid、key（明文）+ trade_no 或 out_trade_no 二选一',
    returns: 'code=1，trade_no、out_trade_no、type、name、money、status、addtime、endtime、param、buyer',
  },
  {
    act: 'orders',
    title: '订单列表',
    params: 'pid、key + 可选 offset / limit / status',
    returns: 'code=1，count（总数）、data[]（单个元素字段同 act=order）',
  },
  {
    act: 'refund',
    title: '发起退款',
    params: 'pid、key + trade_no 或 out_trade_no + money（元，最多两位小数）',
    returns: 'code=0 表示退款申请成功（进入审核/账本流程）；金额超出可退范围返回 code=-1',
  },
  {
    act: 'settle',
    title: '结算记录',
    params: 'pid、key + 可选 offset / limit',
    returns: 'code=1，data[]：结算单号、金额、状态、创建时间',
  },
];

const v1ReturnCodes = [
  { code: '1', desc: '成功（V1 下单/查询类接口）' },
  { code: '0', desc: '成功（V1 退款类接口，如 act=refund）' },
  { code: '-1', desc: '业务失败：参数错误、订单不存在、金额不符、状态不允许等，看 msg' },
  { code: '-2', desc: '接口未开启或无可用通道' },
  { code: '-3', desc: '验签失败 / 密钥错误 / 时间戳超出窗口' },
  { code: '-4', desc: '必填参数缺失' },
  { code: '-5', desc: '无此接口（act 不存在）' },
];

const v2Methods = [
  {
    method: 'pay/submit',
    required: 'pid, out_trade_no, notify_url, name, money, timestamp, sign',
    desc: '页面跳转下单，成功直接 302 到 /cashier.php?trade_no=...；type 留空则进收银台选择支付方式',
  },
  {
    method: 'pay/create',
    required: '同上，可加 method(web/jump/jsapi/scan/app)、clientip',
    desc: 'JSON 下单，返回 pay_type 与 pay_info（二维码链接、跳转 URL 或文本），便于自行渲染收银台',
  },
  {
    method: 'pay/query',
    required: 'pid + trade_no 或 out_trade_no',
    desc: '订单查询：金额、状态、支付时间、渠道单号',
  },
  {
    method: 'pay/close',
    required: 'pid + trade_no 或 out_trade_no',
    desc: '关闭未支付订单；已支付订单会返回业务失败',
  },
  {
    method: 'pay/refund',
    required: 'pid + 订单号 + money（可选 out_refund_no）',
    desc: '创建退款申请，进入审核/账本流程；同一 out_refund_no 幂等',
  },
  {
    method: 'pay/refundquery',
    required: 'pid + trade_no / out_trade_no / refund_no',
    desc: '退款查询：退款单号、金额、状态',
  },
  {
    method: 'merchant/info',
    required: 'pid',
    desc: '商户账户信息：可用余额、冻结余额、订单数',
  },
  {
    method: 'merchant/orders',
    required: 'pid + 可选 page / page_size / status',
    desc: '订单分页列表',
  },
  {
    method: 'transfer/submit',
    required: 'pid, out_transfer_no, money, account_name, account_no, bank_name',
    desc: '代付申请，扣减可用余额或冻结；重复 out_transfer_no 幂等',
  },
  {
    method: 'transfer/query',
    required: 'pid + transfer_no 或 out_transfer_no',
    desc: '代付查询：金额、状态、处理时间',
  },
  {
    method: 'transfer/proof',
    required: 'pid + 代付单号',
    desc: '代付凭证（当前返回代付状态，凭证文件后续开放）',
  },
  {
    method: 'transfer/balance',
    required: 'pid',
    desc: '可提现余额',
  },
];

const v2CommonParams: ParamRow[] = [
  { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
  { name: 'timestamp', type: 'string', required: '是', desc: '10 位秒级时间戳，服务端校验 |now - timestamp| ≤ 300 秒' },
  { name: 'sign', type: 'string', required: '是', desc: '商户私钥对请求参数的 RSA-SHA256 签名（Base64）' },
  { name: 'sign_type', type: 'string', required: '是', desc: '固定 RSA（V2 不接受 MD5）' },
];

const v2ParamGroups: ParamGroup[] = [
  {
    title: 'pay/submit 与 pay/create',
    summary: '下单接口，V2 使用 RSA 签名；金额以订单创建时为准，创建后不可修改。',
    rows: [
      { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
      { name: 'out_trade_no', type: 'string', required: '是', desc: '商户订单号；同号不同金额返回 -1' },
      { name: 'notify_url', type: 'string', required: '是', desc: '异步通知地址' },
      { name: 'return_url', type: 'string', required: '否', desc: '同步跳转地址' },
      { name: 'name', type: 'string', required: '是', desc: '商品名称' },
      { name: 'money', type: 'string', required: '是', desc: '金额（元），0.01 ~ 100000.00，最多两位小数' },
      { name: 'type', type: 'string', required: '否', desc: '支付类型，留空进收银台' },
      { name: 'method', type: 'string', required: '否', desc: 'pay/create 专用：web / jump / jsapi / scan / app' },
      { name: 'clientip', type: 'string', required: '否', desc: '用户客户端 IP' },
      { name: 'param', type: 'string', required: '否', desc: '自定义参数，通知中原样回传' },
      { name: 'timestamp', type: 'string', required: '是', desc: '秒级时间戳' },
      { name: 'sign', type: 'string', required: '是', desc: 'RSA-SHA256 签名' },
      { name: 'sign_type', type: 'string', required: '是', desc: 'RSA' },
    ],
  },
  {
    title: 'pay/query 与 pay/close',
    summary: '查询与关单，只传订单标识。',
    rows: [
      { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
      { name: 'trade_no', type: 'string', required: '否', desc: '平台订单号，与 out_trade_no 二选一' },
      { name: 'out_trade_no', type: 'string', required: '否', desc: '商户订单号，与 trade_no 二选一' },
      { name: 'timestamp', type: 'string', required: '是', desc: '秒级时间戳' },
      { name: 'sign', type: 'string', required: '是', desc: 'RSA-SHA256 签名' },
      { name: 'sign_type', type: 'string', required: '是', desc: 'RSA' },
    ],
  },
  {
    title: 'pay/refund 与 pay/refundquery',
    summary: '退款绑定原支付订单，累计退款不得超过订单实付金额。',
    rows: [
      { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
      { name: 'trade_no', type: 'string', required: '否', desc: '平台订单号' },
      { name: 'out_trade_no', type: 'string', required: '否', desc: '商户订单号' },
      { name: 'money', type: 'string', required: 'pay/refund 必填', desc: '退款金额（元），不得超过剩余可退金额' },
      { name: 'out_refund_no', type: 'string', required: '否', desc: '商户退款单号，重复提交幂等' },
      { name: 'refund_no', type: 'string', required: 'refundquery 可选', desc: '平台退款单号' },
      { name: 'reason', type: 'string', required: '否', desc: '退款原因' },
      { name: 'timestamp', type: 'string', required: '是', desc: '秒级时间戳' },
      { name: 'sign', type: 'string', required: '是', desc: 'RSA-SHA256 签名' },
      { name: 'sign_type', type: 'string', required: '是', desc: 'RSA' },
    ],
  },
  {
    title: 'merchant/info 与 merchant/orders',
    summary: '账户与订单查询，只读接口。',
    rows: [
      { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
      { name: 'page', type: 'int', required: '否', desc: '页码，从 1 开始（merchant/orders）' },
      { name: 'page_size', type: 'int', required: '否', desc: '每页条数，默认 20，最大 100' },
      { name: 'status', type: 'string', required: '否', desc: '订单状态过滤：pending / processing / paid / closed / failed' },
      { name: 'timestamp', type: 'string', required: '是', desc: '秒级时间戳' },
      { name: 'sign', type: 'string', required: '是', desc: 'RSA-SHA256 签名' },
      { name: 'sign_type', type: 'string', required: '是', desc: 'RSA' },
    ],
  },
  {
    title: 'transfer/submit',
    summary: '代付申请，出款前会校验可用余额，重复单号幂等。',
    rows: [
      { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
      { name: 'out_transfer_no', type: 'string', required: '是', desc: '商户代付单号，同一商户下唯一' },
      { name: 'money', type: 'string', required: '是', desc: '代付金额（元）' },
      { name: 'account_name', type: 'string', required: '是', desc: '收款人姓名' },
      { name: 'account_no', type: 'string', required: '是', desc: '收款账号' },
      { name: 'bank_name', type: 'string', required: '是', desc: '开户银行' },
      { name: 'branch_name', type: 'string', required: '否', desc: '开户支行' },
      { name: 'timestamp', type: 'string', required: '是', desc: '秒级时间戳' },
      { name: 'sign', type: 'string', required: '是', desc: 'RSA-SHA256 签名' },
      { name: 'sign_type', type: 'string', required: '是', desc: 'RSA' },
    ],
  },
  {
    title: 'transfer/query、transfer/proof 与 transfer/balance',
    summary: '代付查询与余额查询。',
    rows: [
      { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
      { name: 'transfer_no', type: 'string', required: '否', desc: '平台代付单号（query/proof）' },
      { name: 'out_transfer_no', type: 'string', required: '否', desc: '商户代付单号（query）' },
      { name: 'timestamp', type: 'string', required: '是', desc: '秒级时间戳' },
      { name: 'sign', type: 'string', required: '是', desc: 'RSA-SHA256 签名' },
      { name: 'sign_type', type: 'string', required: '是', desc: 'RSA' },
    ],
  },
];

const notifyFields: ParamRow[] = [
  { name: 'pid', type: 'int', required: '是', desc: '商户 ID' },
  { name: 'trade_no', type: 'string', required: '是', desc: '平台订单号' },
  { name: 'out_trade_no', type: 'string', required: '是', desc: '商户订单号' },
  { name: 'api_trade_no', type: 'string', required: '否', desc: '上游渠道单号' },
  { name: 'type', type: 'string', required: '是', desc: '支付类型：alipay / wxpay / qqpay / bank / sandbox' },
  { name: 'name', type: 'string', required: '是', desc: '商品名称' },
  { name: 'money', type: 'string', required: '是', desc: '订单金额（元），必须与订单金额完全一致' },
  { name: 'trade_status', type: 'string', required: '是', desc: 'TRADE_SUCCESS 表示支付成功' },
  { name: 'param', type: 'string', required: '否', desc: '下单时传入的自定义参数' },
  { name: 'buyer', type: 'string', required: '否', desc: '付款方标识（渠道返回的买家账号）' },
  { name: 'timestamp', type: 'string', required: 'V2 是', desc: '通知时间戳（秒），V2 商户必带' },
  { name: 'sign', type: 'string', required: '是', desc: '签名：V1 商户为 MD5，V2 商户为平台私钥 RSA 签名' },
  { name: 'sign_type', type: 'string', required: 'V2 是', desc: 'MD5 或 RSA' },
];

const v1CodeExample = `curl -X POST 'https://pay.example.com/mapi.php' \\
  -d 'pid=10001' \\
  -d 'type=alipay' \\
  -d 'out_trade_no=ORDER20261113001' \\
  -d 'notify_url=https://shop.example.com/epay/notify' \\
  -d 'return_url=https://shop.example.com/epay/return' \\
  -d 'name=年度会员' \\
  -d 'money=29.90' \\
  -d 'sign_type=MD5' \\
  -d 'sign=8f14e45fceea167a5a36dedd4bea2543'`;

const v1ResponseExample = `{
  "code": 1,
  "trade_no": "2026111322001489",
  "pay_type": "qrcode",
  "pay_info": "https://pay.example.com/qrcode/2026111322001489.png"
}`;

const v1ErrorExample = `{
  "code": -3,
  "msg": "签名校验失败"
}`;

const v2RequestExample = `POST /api/pay/create HTTP/1.1
Host: pay.example.com
Content-Type: application/json

{
  "pid": 10001,
  "out_trade_no": "ORDER20261113001",
  "notify_url": "https://shop.example.com/epay/notify",
  "name": "年度会员",
  "money": "29.90",
  "type": "alipay",
  "method": "web",
  "timestamp": "1794556800",
  "sign_type": "RSA",
  "sign": "Base64 编码的 SHA256withRSA 签名"
}`;

const v2ResponseExample = `{
  "code": 0,
  "trade_no": "2026111322001489",
  "out_trade_no": "ORDER20261113001",
  "status": "pending",
  "pay_type": "qrcode",
  "pay_info": "https://pay.example.com/qrcode/2026111322001489.png",
  "timestamp": "1794556802",
  "sign_type": "RSA",
  "sign": "平台私钥对除 sign/sign_type 外全部字段的签名"
}`;

const v2FailExample = `{
  "code": -3,
  "msg": "时间戳字段不正确",
  "timestamp": "1794556802",
  "sign_type": "RSA",
  "sign": "即使失败响应也带平台签名"
}`;

const signStringExample =
  'money=29.90&name=年度会员&notify_url=https://shop.example.com/epay/notify&out_trade_no=ORDER20261113001&pid=10001&timestamp=1794556800&type=alipay';

const md5Pseudo = `# 伪代码（V1，MD5）
params = 全部请求参数
delete params["sign"], params["sign_type"]
params = drop_empty(params)              # 去掉 '' 与 null，数组值不参与签名
keys = sort_ascii_asc(keys(params))
sign_str = join("&", ["%s=%s" % (k, params[k]) for k in keys])
sign = lowercase(md5(sign_str + merchant_md5_key))`;

const rsaPseudo = `# 伪代码（V2，RSA-SHA256）
params = 全部请求参数（含 timestamp）
delete params["sign"], params["sign_type"]
sign_str = same_rule_as_v1(params)       # 注意：不再追加 key
raw = SHA256withRSA(sign_str, merchant_private_key)
sign = base64(raw)

# 验平台响应/通知（用平台公钥）
ok = RSA_verify(SHA256(sign_str), base64_decode(sign), platform_public_key)`;

const notifyExample = `POST /epay/notify HTTP/1.1
Content-Type: application/x-www-form-urlencoded

pid=10001
&trade_no=2026111322001489
&out_trade_no=ORDER20261113001
&api_trade_no=202611132200148901
&type=alipay
&name=年度会员
&money=29.90
&trade_status=TRADE_SUCCESS
&param=
&buyer=alipay_user@example.com
&timestamp=1794556860
&sign_type=RSA
&sign=Base64 编码的平台签名`;

const notifyReceipt = `# 处理成功必须原样输出（不要带 HTML、空格或其他字符）
success`;

const phpExample = `<?php
// 1) 通用：生成待签名串（V1 / V2 规则一致，V2 不追加 key）
function epay_sign_content(array $params): string
{
    unset($params['sign'], $params['sign_type']);
    $pairs = [];
    foreach ($params as $key => $value) {
        if (is_array($value) || $value === '' || $value === null) {
            continue;
        }
        $pairs[$key] = (string) $value;
    }
    ksort($pairs, SORT_STRING);
    $items = [];
    foreach ($pairs as $key => $value) {
        $items[] = $key . '=' . $value;
    }
    return implode('&', $items);
}

$merchantKey = '你的商户MD5密钥';
$post = $_POST;

// 2) V1：追加商户密钥后 MD5 小写
$v1Sign = md5(epay_sign_content($post) . $merchantKey);

// 3) V2：商户私钥 SHA256withRSA + Base64
$signStr = epay_sign_content($post);
$privateKey = openssl_pkey_get_private(file_get_contents('merchant_private.pem'));
openssl_sign($signStr, $rawSign, $privateKey, OPENSSL_ALGO_SHA256);
$v2Sign = base64_encode($rawSign);

// 4) 校验异步通知（V2 商户用平台公钥）
$platformPub = openssl_pkey_get_public(file_get_contents('platform_public.pem'));
$verifyStr = epay_sign_content($post);
$signature = base64_decode($post['sign'] ?? '', true);
$verified = openssl_verify($verifyStr, $signature, $platformPub, OPENSSL_ALGO_SHA256) === 1;

if (!$verified) {
    exit('fail');
}
if (($post['trade_status'] ?? '') !== 'TRADE_SUCCESS') {
    exit('success');
}
// 金额必须与本地订单金额（分）一致，否则不入账
if ((int) round(((float) $post['money']) * 100) !== (int) $order['amount']) {
    exit('fail');
}
// 幂等：trade_no 已处理过则直接 success
echo 'success';`;

const curlV2Example = `# 1) 生成待签名串（按 ASCII 升序，剔除 sign/sign_type/空值）
SIGN_STR='money=29.90&name=年度会员&out_trade_no=ORDER20261113001&pid=10001&timestamp=1794556800'
printf '%s' "$SIGN_STR" > /tmp/epay.txt

# 2) 商户私钥签名并 Base64
openssl dgst -sha256 -sign merchant_private.pem -out /tmp/epay.sig /tmp/epay.txt
SIGN=$(openssl base64 -A -in /tmp/epay.sig)

# 3) 组装 JSON 并调用 V2
cat > /tmp/req.json <<EOF
{"pid":10001,"out_trade_no":"ORDER20261113001","timestamp":"1794556800","sign_type":"RSA","sign":"\${SIGN}"}
EOF

curl -X POST 'https://pay.example.com/api/pay/query' \\
  -H 'Content-Type: application/json' \\
  --data @/tmp/req.json`;

const goExample = `package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

// 生成待签名串：剔除 sign/sign_type 与空值，按参数名 ASCII 升序拼接
func signContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("invalid pem")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// V2 请求签名
func rsaSign(content string, priv *rsa.PrivateKey) (string, error) {
	sum := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// V2 校验平台响应/通知
func rsaVerify(content, sign string, pub *rsa.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], raw)
}

func main() {
	priv, err := loadPrivateKey("merchant_private.pem")
	if err != nil {
		log.Fatal(err)
	}
	params := map[string]string{
		"pid":          "10001",
		"out_trade_no": "ORDER20261113001",
		"name":         "年度会员",
		"money":        "29.90",
		"type":         "alipay",
		"notify_url":   "https://shop.example.com/epay/notify",
		"timestamp":    "1794556800",
		"sign_type":    "RSA",
	}
	sign, err := rsaSign(signContent(params), priv)
	if err != nil {
		log.Fatal(err)
	}
	params["sign"] = sign
	fmt.Println("sign:", sign)
}`;
</script>

<template>
  <div class="docs">
    <PublicNav />
    <div class="docs-shell">
      <header class="docs-head">
        <span class="docs-badge">易支付协议 · V1 + V2</span>
        <h1 class="docs-title">接入文档</h1>
        <p class="docs-subtitle">
          兼容彩虹易支付 V1 全部对外接口，并提供 V2 的 RSA-SHA256 双向签名、时间戳防重放与订单金额锁定。
          所有金额参数单位为「元」，最多两位小数；平台内部按「分」记账。
        </p>
      </header>

      <n-tabs type="line" animated default-value="sign" class="docs-tabs">
        <n-tab-pane name="sign" tab="签名规则">
          <section class="doc-section">
            <h2 class="doc-h2">待签名串生成（V1 / V2 通用）</h2>
            <ol class="doc-list">
              <li>取本次请求（或响应）全部参数，剔除 <code>sign</code>、<code>sign_type</code> 以及空值（空字符串 / null），数组值不参与签名。</li>
              <li>按参数名 ASCII 升序排序（区分大小写，<code>ksort(SORT_STRING)</code> 等价）。</li>
              <li>拼接为 <code>k1=v1&amp;k2=v2...</code>，末尾不带 <code>&amp;</code>，值使用原始值，不要先做 URL 编码。</li>
            </ol>
            <h3 class="doc-h3">示例待签名串</h3>
            <pre class="doc-code">{{ signStringExample }}</pre>

            <h2 class="doc-h2">V1：MD5 签名</h2>
            <p class="doc-p">
              在待签名串末尾<strong>直接拼接商户 MD5 密钥</strong>（无分隔符），取 MD5 后转小写。V1 的
              <code>submit.php</code> / <code>mapi.php</code> 也支持 <code>sign_type=RSA</code>（商户公钥验签）。
            </p>
            <pre class="doc-code">{{ md5Pseudo }}</pre>
            <n-alert type="warning" :bordered="false" class="doc-alert">
              V1 历史问题：下单签名与异步通知签名使用同一套 MD5 规则，密钥泄露或签名被截获可能被重放。
              V2 起改用双向 RSA 签名 + 时间戳窗口，彻底隔离「商户签名」与「平台签名」。
            </n-alert>

            <h2 class="doc-h2">V2：RSA-SHA256 签名</h2>
            <ul class="doc-list">
              <li>商户用<strong>自己的 RSA 私钥</strong>对请求待签名串做 <code>SHA256withRSA</code>，结果 Base64 编码放入 <code>sign</code>，<code>sign_type=RSA</code>。</li>
              <li>待签名串规则与 V1 相同，但<strong>不再追加任何 key</strong>。</li>
              <li>平台用<strong>平台私钥</strong>对响应与异步通知签名，商户用平台公钥验签（双向签名）。</li>
              <li>请求必须携带 10 位秒级 <code>timestamp</code>，服务端校验 <code>|now - timestamp| ≤ 300</code> 秒；超出窗口返回 <code>code=-3</code>。</li>
              <li>同一 <code>sign</code> 只能使用一次（服务端按 <code>merchant_id + sign</code> 记录 nonce），重复提交返回 <code>code=-3</code>。</li>
            </ul>
            <pre class="doc-code">{{ rsaPseudo }}</pre>
          </section>
        </n-tab-pane>

        <n-tab-pane name="v1" tab="V1 接口">
          <section class="doc-section">
            <h2 class="doc-h2">submit.php：页面跳转支付</h2>
            <p class="doc-p">
              浏览器表单 / GET 提交，验签通过后 302 跳转到收银台；参数错误时返回 HTML 提示页。适合传统 PHP 站点直接接入。
            </p>
            <table class="doc-table">
              <thead>
                <tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in v1SubmitParams" :key="`submit-${row.name}`">
                  <td class="mono">{{ row.name }}</td>
                  <td>{{ row.type }}</td>
                  <td>{{ row.required }}</td>
                  <td>{{ row.desc }}</td>
                </tr>
              </tbody>
            </table>

            <h2 class="doc-h2">mapi.php：接口下单</h2>
            <p class="doc-p">
              返回 JSON，适合在自有收银台自行渲染二维码或跳转按钮。成功：<code>code=1</code>；失败：<code>code&lt;0</code> 并返回
              <code>msg</code>。
            </p>
            <table class="doc-table">
              <thead>
                <tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in v1MapiParams" :key="`mapi-${row.name}`">
                  <td class="mono">{{ row.name }}</td>
                  <td>{{ row.type }}</td>
                  <td>{{ row.required }}</td>
                  <td>{{ row.desc }}</td>
                </tr>
              </tbody>
            </table>
            <div class="doc-code-pair">
              <div>
                <div class="doc-code-label">成功响应</div>
                <pre class="doc-code">{{ v1ResponseExample }}</pre>
              </div>
              <div>
                <div class="doc-code-label">失败响应</div>
                <pre class="doc-code">{{ v1ErrorExample }}</pre>
              </div>
            </div>

            <h2 class="doc-h2">api.php：查询 / 退款 / 结算</h2>
            <p class="doc-p">
              <code>GET|POST /api.php?act=query|order|orders|refund|settle</code>。支持 <code>pid + key</code> 明文模式（便于兼容老系统）
              与 <code>pid + timestamp + sign</code> 签名模式。
            </p>
            <table class="doc-table">
              <thead>
                <tr><th>act</th><th>用途</th><th>主要参数</th><th>返回</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in v1ApiActs" :key="row.act">
                  <td class="mono">{{ row.act }}</td>
                  <td>{{ row.title }}</td>
                  <td>{{ row.params }}</td>
                  <td>{{ row.returns }}</td>
                </tr>
              </tbody>
            </table>

            <h2 class="doc-h2">V1 返回码</h2>
            <table class="doc-table">
              <thead>
                <tr><th>code</th><th>含义</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in v1ReturnCodes" :key="row.code">
                  <td class="mono">{{ row.code }}</td>
                  <td>{{ row.desc }}</td>
                </tr>
              </tbody>
            </table>
          </section>
        </n-tab-pane>

        <n-tab-pane name="v2" tab="V2 接口">
          <section class="doc-section">
            <h2 class="doc-h2">协议形式</h2>
            <p class="doc-p">
              <code>/api/:class/:method</code>，同时兼容 <code>api.php?s=pay/query</code> 形式（等价同一 handler）。
              请求与响应均为 JSON，金额单位为元（字符串或数字均可，服务端按分解析）。
            </p>
            <div class="doc-code-pair">
              <div>
                <div class="doc-code-label">请求示例（pay/create）</div>
                <pre class="doc-code">{{ v2RequestExample }}</pre>
              </div>
              <div>
                <div class="doc-code-label">响应信封（code=0 + 平台签名）</div>
                <pre class="doc-code">{{ v2ResponseExample }}</pre>
              </div>
            </div>
            <n-alert type="info" :bordered="false" class="doc-alert">
              所有 V2 响应（包括失败响应）都追加 <code>timestamp</code>、<code>sign_type:"RSA"</code>、<code>sign</code>，
              其中 <code>sign</code> 是平台私钥对「除 sign / sign_type 外全部响应字段」的签名，商户必须验签后再信任内容。
            </n-alert>
            <pre class="doc-code">{{ v2FailExample }}</pre>

            <h2 class="doc-h2">方法总览</h2>
            <table class="doc-table">
              <thead>
                <tr><th>方法</th><th>必填参数</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in v2Methods" :key="row.method">
                  <td class="mono">{{ row.method }}</td>
                  <td class="mono small">{{ row.required }}</td>
                  <td>{{ row.desc }}</td>
                </tr>
              </tbody>
            </table>

            <h2 class="doc-h2">通用参数</h2>
            <table class="doc-table">
              <thead>
                <tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in v2CommonParams" :key="`common-${row.name}`">
                  <td class="mono">{{ row.name }}</td>
                  <td>{{ row.type }}</td>
                  <td>{{ row.required }}</td>
                  <td>{{ row.desc }}</td>
                </tr>
              </tbody>
            </table>

            <template v-for="group in v2ParamGroups" :key="group.title">
              <h2 class="doc-h2">{{ group.title }}</h2>
              <p class="doc-p">{{ group.summary }}</p>
              <table class="doc-table">
                <thead>
                  <tr><th>参数</th><th>类型</th><th>必填</th><th>说明</th></tr>
                </thead>
                <tbody>
                  <tr v-for="row in group.rows" :key="`${group.title}-${row.name}`">
                    <td class="mono">{{ row.name }}</td>
                    <td>{{ row.type }}</td>
                    <td>{{ row.required }}</td>
                    <td>{{ row.desc }}</td>
                  </tr>
                </tbody>
              </table>
            </template>
          </section>
        </n-tab-pane>

        <n-tab-pane name="notify" tab="异步通知">
          <section class="doc-section">
            <h2 class="doc-h2">通知字段</h2>
            <p class="doc-p">
              支付成功后平台以 <code>application/x-www-form-urlencoded</code> POST 到下单时的 <code>notify_url</code>。
              V1 商户使用 MD5 签名（<code>sign_type</code> 可能缺省），V2 商户使用平台私钥 RSA 签名并带 <code>timestamp</code>。
            </p>
            <table class="doc-table">
              <thead>
                <tr><th>字段</th><th>类型</th><th>必返</th><th>说明</th></tr>
              </thead>
              <tbody>
                <tr v-for="row in notifyFields" :key="`notify-${row.name}`">
                  <td class="mono">{{ row.name }}</td>
                  <td>{{ row.type }}</td>
                  <td>{{ row.required }}</td>
                  <td>{{ row.desc }}</td>
                </tr>
              </tbody>
            </table>

            <h2 class="doc-h2">通知示例</h2>
            <pre class="doc-code">{{ notifyExample }}</pre>

            <h2 class="doc-h2">处理要求</h2>
            <ol class="doc-list">
              <li>先验签：V1 用商户 MD5 密钥重算，V2 用平台公钥验 <code>sign</code>；验签失败直接返回 <code>fail</code>。</li>
              <li>校验 <code>trade_status === 'TRADE_SUCCESS'</code>（或渠道约定的成功值）。</li>
              <li>用 <code>out_trade_no</code> 查本地订单，核对 <code>money</code> 与订单金额（分）完全一致；不一致按伪造处理。</li>
              <li>按 <code>trade_no</code> 做幂等：已处理的订单直接返回 <code>success</code>，不要重复发货/入账。</li>
              <li>处理成功后<strong>原样输出 <code>success</code></strong>（无 HTML、无空格、无 BOM），否则平台会退避重试。</li>
            </ol>
            <pre class="doc-code">{{ notifyReceipt }}</pre>
            <n-alert type="warning" :bordered="false" class="doc-alert">
              通知可能重复、延迟或乱序到达。必须以订单金额与状态为准做幂等，不要依赖通知次数或到达顺序。
            </n-alert>
          </section>
        </n-tab-pane>

        <n-tab-pane name="security" tab="安全说明">
          <section class="doc-section">
            <h2 class="doc-h2">签名方向</h2>
            <ul class="doc-list">
              <li>商户 → 平台：商户私钥签名，平台用<strong>商户公钥</strong>验签（V1 为 MD5 共享密钥）。</li>
              <li>平台 → 商户：平台私钥签名响应与通知，商户用<strong>平台公钥</strong>验签。</li>
              <li>两套密钥互不复用，平台私钥永不外泄；商户 V2 私钥仅在生成/轮换时展示一次。</li>
            </ul>

            <h2 class="doc-h2">请求防重放</h2>
            <ul class="doc-list">
              <li>V2 请求必须带 10 位秒级 <code>timestamp</code>，服务端校验 <code>|now - timestamp| ≤ 300</code> 秒。</li>
              <li>服务端记录 <code>(merchant_id, sign)</code> 一次性 nonce，同一签名重复提交直接拒绝（<code>code=-3</code>）。</li>
              <li>建议商户侧也校验响应 <code>timestamp</code> 与签名，避免中间人替换响应内容。</li>
            </ul>

            <h2 class="doc-h2">金额以订单为准</h2>
            <ul class="doc-list">
              <li>下单时金额写入订单，<strong>创建后不可修改</strong>；同一 <code>out_trade_no</code> 提交不同金额返回 <code>code=-1</code>。</li>
              <li>渠道回调的金额必须与订单金额完全一致，否则拒绝入账（409）。</li>
              <li>收银台发起支付、沙箱支付接口都<strong>不接受客户端传入金额</strong>，只按订单金额扣款。</li>
              <li>退款累计金额不得超过订单实付金额；代付必须扣减可用余额或冻结，重复单号幂等。</li>
            </ul>

            <h2 class="doc-h2">杜绝 0 元入账</h2>
            <ul class="doc-list">
              <li>金额统一按「分」解析：拒绝 0、负数、超过两位小数、科学计数法（如 <code>1e3</code>）与空值。</li>
              <li>单笔金额范围 0.01 ~ 100000.00 元（上限可由平台配置）。</li>
              <li>只有渠道回调 / 沙箱完成能把订单置为 <code>paid</code>，没有「手动标记已支付」接口。</li>
              <li>沙箱渠道仅在开发环境且显式开启时注册，生产环境默认关闭。</li>
            </ul>

            <h2 class="doc-h2">密钥管理建议</h2>
            <ul class="doc-list">
              <li>MD5 密钥定期轮换，轮换后旧密钥立即失效，注意灰度切换时间窗。</li>
              <li>V2 私钥生成后只展示一次，请立即保存到密钥管理系统；丢失只能重新生成。</li>
              <li>私钥不要提交到 Git、不要放进前端或移动端包体，签名动作只在服务端完成。</li>
            </ul>
          </section>
        </n-tab-pane>

        <n-tab-pane name="code" tab="代码示例">
          <section class="doc-section">
            <h2 class="doc-h2">curl：V1 接口下单</h2>
            <pre class="doc-code">{{ v1CodeExample }}</pre>

            <h2 class="doc-h2">curl：V2 RSA 签名</h2>
            <pre class="doc-code">{{ curlV2Example }}</pre>

            <h2 class="doc-h2">PHP：签名与通知验签</h2>
            <pre class="doc-code">{{ phpExample }}</pre>

            <h2 class="doc-h2">Go：签名与验签</h2>
            <pre class="doc-code">{{ goExample }}</pre>
          </section>
        </n-tab-pane>
      </n-tabs>

      <footer class="docs-foot">
        <span>文档版本：V2 · 2026-11</span>
        <span class="docs-foot-links">
          <router-link to="/register">免费注册</router-link>
          <span class="sep">·</span>
          <router-link to="/login">商户登录</router-link>
        </span>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.docs {
  background: #fff;
  min-height: 100vh;
}
.docs-shell {
  max-width: 1080px;
  margin: 0 auto;
  padding: 40px 20px 60px;
}
.docs-head {
  margin-bottom: 26px;
}
.docs-badge {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 999px;
  background: rgba(37, 99, 235, 0.1);
  color: #1d4ed8;
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 14px;
}
.docs-title {
  font-size: 32px;
  margin: 0 0 10px;
  color: #0f172a;
}
.docs-subtitle {
  margin: 0;
  color: #475569;
  font-size: 14px;
  line-height: 1.85;
  max-width: 860px;
}
.docs-tabs {
  margin-top: 8px;
}
.doc-section {
  padding: 18px 2px 10px;
}
.doc-h2 {
  font-size: 18px;
  color: #0f172a;
  margin: 26px 0 10px;
  padding-left: 10px;
  border-left: 3px solid #2563eb;
}
.doc-h3 {
  font-size: 15px;
  color: #1e293b;
  margin: 18px 0 8px;
}
.doc-p {
  color: #475569;
  font-size: 13.5px;
  line-height: 1.9;
  margin: 0 0 12px;
}
.doc-list {
  color: #475569;
  font-size: 13.5px;
  line-height: 1.95;
  padding-left: 22px;
  margin: 0 0 14px;
}
.doc-list li {
  margin-bottom: 4px;
}
.doc-list code,
.doc-p code {
  background: #f1f5f9;
  padding: 1px 6px;
  border-radius: 5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  color: #1d4ed8;
}
.doc-table {
  width: 100%;
  border-collapse: collapse;
  margin: 6px 0 18px;
  font-size: 13px;
  background: #fff;
  border: 1px solid #e8eefc;
  border-radius: 10px;
  overflow: hidden;
}
.doc-table th {
  text-align: left;
  background: #f8fafc;
  color: #334155;
  font-weight: 600;
  padding: 10px 12px;
  border-bottom: 1px solid #e8eefc;
  white-space: nowrap;
}
.doc-table td {
  padding: 10px 12px;
  border-bottom: 1px solid #f1f5f9;
  color: #475569;
  line-height: 1.7;
  vertical-align: top;
}
.doc-table tr:last-child td {
  border-bottom: none;
}
.doc-table .mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: #1e293b;
  white-space: nowrap;
}
.doc-table .mono.small {
  white-space: normal;
  font-size: 12px;
  color: #64748b;
}
.doc-code {
  background: #0f172a;
  color: #dbeafe;
  border-radius: 10px;
  padding: 14px 16px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.2px;
  line-height: 1.75;
  overflow-x: auto;
  white-space: pre;
  margin: 0 0 16px;
}
.doc-code-label {
  font-size: 12px;
  color: #64748b;
  margin-bottom: 6px;
}
.doc-code-pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.doc-alert {
  margin: 6px 0 18px;
}
.docs-foot {
  margin-top: 34px;
  padding-top: 18px;
  border-top: 1px solid #eef2f7;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 12.5px;
  color: #94a3b8;
}
.docs-foot-links {
  display: flex;
  gap: 8px;
}
.docs-foot-links a:hover {
  color: #2563eb;
}
.sep {
  color: #cbd5e1;
}
@media (max-width: 768px) {
  .docs-shell {
    padding: 26px 14px 40px;
  }
  .docs-title {
    font-size: 24px;
  }
  .doc-code-pair {
    grid-template-columns: minmax(0, 1fr);
  }
  .doc-table {
    display: block;
    overflow-x: auto;
  }
  .doc-table th,
  .doc-table td {
    white-space: nowrap;
  }
  .doc-table td {
    white-space: normal;
  }
}
</style>
