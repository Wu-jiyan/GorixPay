import React, { useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Card, Spin, Message, Result } from '@arco-design/web-react';
import axios from 'axios';

interface OrderInfo {
  trade_no: string;
  amount: number;
  name: string;
  status: string;
  payment_type: string;
  pay_info?: string;
}

const Cashier: React.FC = () => {
  const { tradeNo } = useParams<{ tradeNo: string }>();
  const [order, setOrder] = useState<OrderInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<'loading' | 'paying' | 'paid' | 'timeout' | 'error'>('loading');
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const startTimeRef = useRef<number>(Date.now());

  useEffect(() => {
    if (!tradeNo) {
      setStatus('error');
      setLoading(false);
      return;
    }

    axios
      .get<OrderInfo>('/api/v1/cashier/orders/' + tradeNo)
      .then((res) => {
        setOrder(res.data);
        setLoading(false);

        if (res.data.payment_type === 'jump') {
          setStatus('paying');
          return;
        }

        setStatus('paying');
        startTimeRef.current = Date.now();

        intervalRef.current = setInterval(async () => {
          try {
            const pollRes = await axios.get<OrderInfo>('/api/v1/cashier/orders/' + tradeNo);
            if (pollRes.data.status === 'paid') {
              setStatus('paid');
              if (intervalRef.current) clearInterval(intervalRef.current);
              return;
            }
            if (Date.now() - startTimeRef.current > 5 * 60 * 1000) {
              setStatus('timeout');
              if (intervalRef.current) clearInterval(intervalRef.current);
            }
          } catch {
            // ignore poll errors
          }
        }, 3000);
      })
      .catch(() => {
        setStatus('error');
        setLoading(false);
      });

    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [tradeNo]);

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Spin loading tip="加载订单信息..." />
      </div>
    );
  }

  if (status === 'error' || !order) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Result status="error" title="订单不存在" subTitle="请检查支付链接是否正确" />
      </div>
    );
  }

  if (status === 'paid') {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Result status="success" title="支付成功" subTitle={`订单 ${order.trade_no} 已支付 ¥${(order.amount / 100).toFixed(2)}`} />
      </div>
    );
  }

  if (status === 'timeout') {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Result status="warning" title="支付超时" subTitle="该订单已超过支付有效期" />
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f5f5f5' }}>
      <Card style={{ width: 420, textAlign: 'center' }}>
        <h2>{order.name}</h2>
        <p style={{ fontSize: 28, color: '#f53f3f', fontWeight: 'bold', margin: '16px 0' }}>
          ¥{(order.amount / 100).toFixed(2)}
        </p>
        {order.payment_type === 'jump' ? (
          <p>正在跳转到支付页面...</p>
        ) : order.pay_info ? (
          <div>
            <p>请使用对应 App 扫描二维码支付</p>
            <img src={order.pay_info} alt="支付二维码" style={{ width: 200, height: 200, marginTop: 16 }} />
            <p style={{ marginTop: 12, color: '#999' }}>订单号：{order.trade_no}</p>
          </div>
        ) : (
          <p>等待支付数据...</p>
        )}
      </Card>
    </div>
  );
};

export default Cashier;
