<?php

function is_empty_value($value)
{
    return $value === null || trim($value) === '';
}

function sign_content($data)
{
    ksort($data);
    $parts = [];
    foreach ($data as $key => $value) {
        if (is_array($value) || is_empty_value($value) || $key === 'sign' || $key === 'sign_type') {
            continue;
        }
        $parts[] = $key . '=' . $value;
    }
    return implode('&', $parts);
}

$params = [
    'pid' => 1001,
    'out_trade_no' => 'GXP202607120001',
    'name' => '阶段0测试',
    'amount' => '1.00',
    'sign_type' => 'RSA',
];
$content = sign_content($params);
$privateKey = openssl_pkey_get_private(file_get_contents(__DIR__ . '/../fixtures/test_only_rsa_private.pem'));
$publicKey = openssl_pkey_get_public(file_get_contents(__DIR__ . '/../fixtures/test_only_rsa_public.pem'));
openssl_sign($content, $signature, $privateKey, OPENSSL_ALGO_SHA256);
$encoded = base64_encode($signature);

echo json_encode([
    'canonical' => $content,
    'md5' => md5($content . 'test-key'),
    'rsa' => $encoded,
    'verified' => openssl_verify($content, $signature, $publicKey, OPENSSL_ALGO_SHA256) === 1,
], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
