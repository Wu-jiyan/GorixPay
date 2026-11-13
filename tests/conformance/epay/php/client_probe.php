<?php
/*
 * 真实客户端探针：直接实例化上游自带的易支付客户端 SDK
 * （Epay/plugins/epay/inc/EpayCore.class.php），用它的 curl 请求打到我们
 * 运行中的 Go 服务上。这证明的是"客户端 SDK 原样可用"，不是"我们自己
 * 复刻的签名能和我们对上"。
 *
 * 输入（stdin，JSON）：{"action":"...","upstream_dir":"...","apiurl":"...","pid":...,"key":"...", ...}
 * 输出（stdout，JSON）：{"action":"...","response":...,"url":...,"html":...,"verified":...}
 */

$input = json_decode(file_get_contents('php://stdin'), true);
if (!is_array($input)) {
    fwrite(STDERR, "invalid input\n");
    exit(2);
}

require rtrim($input['upstream_dir'], '/\\') . '/plugins/epay/inc/EpayCore.class.php';

$client = new EpayCore([
    'apiurl' => $input['apiurl'],
    'pid' => $input['pid'],
    'key' => $input['key'],
]);

$output = ['action' => $input['action']];
try {
    switch ($input['action']) {
        case 'apiPay':
            $output['response'] = $client->apiPay($input['params']);
            break;
        case 'buildParams':
            $method = new ReflectionMethod('EpayCore', 'buildRequestParam');
            $method->setAccessible(true);
            $output['params'] = $method->invoke($client, $input['params']);
            $output['body'] = http_build_query($output['params']);
            break;
        case 'payLink':
            $output['url'] = $client->getPayLink($input['params']);
            break;
        case 'pagePay':
            $output['html'] = $client->pagePay($input['params']);
            break;
        case 'queryOrder':
            $output['response'] = $client->queryOrder($input['trade_no']);
            break;
        case 'refund':
            $output['response'] = $client->refund($input['refund_no'], $input['trade_no'], $input['money']);
            break;
        case 'verifyNotify':
            $_GET = $input['params'];
            $output['verified'] = $client->verifyNotify();
            break;
        case 'verifyReturn':
            $_GET = $input['params'];
            $output['verified'] = $client->verifyReturn();
            break;
        default:
            throw new Exception('unknown action: ' . $input['action']);
    }
} catch (Exception $e) {
    fwrite(STDERR, $e->getMessage() . "\n");
    exit(3);
}

echo json_encode($output, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
