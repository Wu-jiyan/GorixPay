import React, { useEffect, useState } from 'react';
import { Card, Statistic, Grid, Spin } from '@arco-design/web-react';
import { adminApi, DashboardData } from '@/api/admin';

const Row = Grid.Row;
const Col = Grid.Col;

const Dashboard: React.FC = () => {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    console.log('[Dashboard] mounting, fetching API...');
    adminApi.dashboard()
      .then((res) => { console.log('[Dashboard] API OK:', res.data); setData(res.data); })
      .catch((err) => { console.error('[Dashboard] API failed:', err?.response?.status, err?.response?.data); })
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <Spin loading style={{ display: 'flex', justifyContent: 'center', marginTop: 100 }} />;

  return (
    <div>
      <Row gutter={16}>
        <Col span={6}><Card><Statistic title="商户数" value={data?.merchant_count || 0} /></Card></Col>
        <Col span={6}><Card><Statistic title="订单数" value={data?.order_count || 0} /></Card></Col>
        <Col span={6}><Card><Statistic title="已支付订单" value={data?.paid_order_count || 0} /></Card></Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="已支付金额"
              value={((data?.paid_amount || 0) / 100).toFixed(2)}
              prefix="¥"
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default Dashboard;
