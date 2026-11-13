<?php
/*
 * 上游签名算法探针。
 *
 * 本文件里的函数体逐字复制自易支付源码，用于把 PHP 的真实计算结果和 Go 实现
 * 做逐字节比对；任何"重新实现"都会让比对失去意义，请勿改写逻辑。
 *
 * 来源：
 *   isEmpty        -> Epay/includes/functions.php:495
 *   getSignContent -> Epay/includes/lib/Payment.php:8
 *   makeSign       -> Epay/includes/lib/Payment.php:20
 *   verifySign     -> Epay/includes/lib/Payment.php:37
 *   base64ToPem    -> Epay/includes/functions.php:1417
 *
 * 输入（stdin，JSON）：
 *   {"mode":"sign","params":{...},"md5key":"...","private_key_file":"...","public_key_file":"..."}
 *   {"mode":"verify","canonical":"...","sign":"...","public_key_file":"..."}
 * 输出（stdout，JSON）：
 *   {"canonical":"...","md5":"...","rsa":"...","php_verified":true}
 *   {"verified":true}
 */

function isEmpty($value)
{
    return $value === null || trim($value) === '';
}

function getSignContent($data)
{
    ksort($data);
    $signStr = '';
    foreach ($data as $k => $v) {
        if (is_array($v) || isEmpty($v) || $k == 'sign' || $k == 'sign_type') continue;
        $signStr .= $k . '=' . $v . '&';
    }
    $signStr = substr($signStr, 0, -1);
    return $signStr;
}

function base64ToPem($data, $type)
{
    if (empty($data) || strpos($data, '-----BEGIN') !== false) return $data;
    $pem = "-----BEGIN " . $type . "-----\n" .
        wordwrap($data, 64, "\n", true) .
        "\n-----END " . $type . "-----";
    return $pem;
}

function pemToBase64($data)
{
    $line = explode("\n", $data);
    $base64 = '';
    foreach ($line as $row) {
        if (empty($row) || strpos($row, '-----BEGIN') !== false || strpos($row, '-----END') !== false) continue;
        $base64 .= trim($row);
    }
    return $base64;
}

// makeSign 的 RSA 分支（Payment.php:20-35），密钥从 PEM 文件读入后按上游方式还原。
function makeSignRsa($data, $pemFile)
{
    $pkey = openssl_pkey_get_private(base64ToPem(pemToBase64(file_get_contents($pemFile)), 'PRIVATE KEY'));
    if (!$pkey) {
        throw new Exception('load private key failed');
    }
    $signStr = getSignContent($data);
    openssl_sign($signStr, $sign, $pkey, OPENSSL_ALGO_SHA256);
    return base64_encode($sign);
}

// makeSign 的 MD5 分支（Payment.php:36-39）。
function makeSignMd5($data, $md5key)
{
    $signStr = getSignContent($data);
    return md5($signStr . $md5key);
}

$input = json_decode(file_get_contents('php://stdin'), true);
if (!is_array($input)) {
    fwrite(STDERR, "invalid input\n");
    exit(2);
}

try {
    if ($input['mode'] === 'verify') {
        $pkey = openssl_pkey_get_public(base64ToPem(pemToBase64(file_get_contents($input['public_key_file'])), 'PUBLIC KEY'));
        if (!$pkey) {
            throw new Exception('load public key failed');
        }
        $result = openssl_verify($input['canonical'], base64_decode($input['sign']), $pkey, OPENSSL_ALGO_SHA256);
        echo json_encode(['verified' => $result === 1]);
        exit(0);
    }

    $params = isset($input['params']) && is_array($input['params']) ? $input['params'] : [];
    $canonical = getSignContent($params);
    $output = ['canonical' => $canonical];
    if (!empty($input['md5key'])) {
        $output['md5'] = makeSignMd5($params, $input['md5key']);
    }
    if (!empty($input['private_key_file'])) {
        $output['rsa'] = makeSignRsa($params, $input['private_key_file']);
        if (!empty($input['public_key_file'])) {
            $pkey = openssl_pkey_get_public(base64ToPem(pemToBase64(file_get_contents($input['public_key_file'])), 'PUBLIC KEY'));
            $output['php_verified'] = openssl_verify($canonical, base64_decode($output['rsa']), $pkey, OPENSSL_ALGO_SHA256) === 1;
        }
    }
    echo json_encode($output, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
} catch (Exception $e) {
    fwrite(STDERR, $e->getMessage() . "\n");
    exit(3);
}
