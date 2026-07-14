import React, { useCallback, useEffect, useState } from 'react';
import { Card, Input, Select, Space, Button } from '@arco-design/web-react';
import { IconSearch } from '@arco-design/web-react/icon';
import { merchantApi, MerchantOrder } from '@/api/merchant';
import DataTable from '@/components/DataTable';
import StatusTag from '@/components/StatusTag';
import { PaginatedResponse } from '@/api/admin';

const MerchantOrders: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<MerchantOrder>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await merchantApi.orders({ page, page_size: 20, keyword: keyword || undefined, status: status || undefined });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page, keyword, status]);

  useEffect(() => { fetch(); }, [fetch]);

  const columns = [
    { title: '平台单号', dataIndex: 'trade_no', width: 180 },
    { title: '商户单号', dataIndex: 'out_trade_no', width: 180 },
    { title: '商品名称', dataIndex: 'name', ellipsis: true },
    { title: '金额', dataIndex: 'amount', render: (_: unknown, r: MerchantOrder) => `${(r.amount / 100).toFixed(2)}` },
    { title: '支付方式', dataIndex: 'payment_type' },
    { title: '状态', dataIndex: 'status', render: (_: unknown, r: MerchantOrder) => <StatusTag status={r.status} /> },
    {
      title: '创建时间', dataIndex: 'created_at',
      render: (_: unknown, r: MerchantOrder) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
  ];

  return (
    <Card title="我的订单">
      <Space style={{ marginBottom: 16 }}>
        <Input prefix={<IconSearch />} placeholder="搜索单号/商品名" value={keyword} onChange={setKeyword} style={{ width: 240 }} allowClear />
        <Select placeholder="状态" value={status || undefined} onChange={(v) => setStatus(v || '')} style={{ width: 120 }} allowClear>
          <Select.Option value="pending">待支付</Select.Option>
          <Select.Option value="paid">已支付</Select.Option>
          <Select.Option value="closed">已关闭</Select.Option>
        </Select>
        <Button type="primary" onClick={fetch}>搜索</Button>
      </Space>
      <DataTable columns={columns} data={data} loading={loading} rowKey="id" onChange={(p) => setPage(p.current || 1)} />
    </Card>
  );
};

export default MerchantOrders;
