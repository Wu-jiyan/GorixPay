import React, { useCallback, useEffect, useState } from 'react';
import { Card, Input, Select, Button, Message, Space, Tag } from '@arco-design/web-react';
import { IconSearch } from '@arco-design/web-react/icon';
import { adminApi, Order, PaginatedResponse } from '@/api/admin';
import DataTable from '@/components/DataTable';
import StatusTag from '@/components/StatusTag';

const Orders: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<Order>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.orders({ page, page_size: 20, keyword: keyword || undefined, status: status || undefined });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page, keyword, status]);

  useEffect(() => { fetch(); }, [fetch]);

  const closeOrder = async (id: string) => {
    try {
      await adminApi.closeOrder(id);
      Message.success('订单已关闭');
      fetch();
    } catch {
      Message.error('操作失败');
    }
  };

  const columns = [
    { title: '平台单号', dataIndex: 'trade_no', width: 180 },
    { title: '商户单号', dataIndex: 'out_trade_no', width: 180 },
    { title: '商品名称', dataIndex: 'name', ellipsis: true },
    { title: '金额', dataIndex: 'amount', render: (_: unknown, r: Order) => `${(r.amount / 100).toFixed(2)}` },
    { title: '支付方式', dataIndex: 'payment_type' },
    { title: '状态', dataIndex: 'status', render: (_: unknown, r: Order) => <StatusTag status={r.status} /> },
    {
      title: '创建时间', dataIndex: 'created_at',
      render: (_: unknown, r: Order) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
    {
      title: '操作', dataIndex: 'id',
      render: (id: string, r: Order) =>
        (r.status === 'pending' || r.status === 'processing') ? (
          <Button size="small" type="text" status="warning" onClick={() => closeOrder(id)}>
            关闭
          </Button>
        ) : <Tag>--</Tag>,
    },
  ];

  return (
    <Card title="订单管理">
      <Space style={{ marginBottom: 16 }}>
        <Input prefix={<IconSearch />} placeholder="搜索单号/商品名" value={keyword} onChange={setKeyword} style={{ width: 240 }} allowClear />
        <Select placeholder="状态" value={status || undefined} onChange={(v) => setStatus(v || '')} style={{ width: 120 }} allowClear>
          <Select.Option value="pending">待支付</Select.Option>
          <Select.Option value="processing">处理中</Select.Option>
          <Select.Option value="paid">已支付</Select.Option>
          <Select.Option value="closed">已关闭</Select.Option>
          <Select.Option value="expired">已过期</Select.Option>
        </Select>
        <Button type="primary" onClick={fetch}>搜索</Button>
      </Space>
      <DataTable
        columns={columns}
        data={data}
        loading={loading}
        rowKey="id"
        onChange={(p) => setPage(p.current || 1)}
      />
    </Card>
  );
};

export default Orders;
