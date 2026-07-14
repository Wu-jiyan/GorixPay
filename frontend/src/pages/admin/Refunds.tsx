import React, { useCallback, useEffect, useState } from 'react';
import { Card, Input, Select, Button, Message, Space, Modal } from '@arco-design/web-react';
import { IconSearch } from '@arco-design/web-react/icon';
import { adminApi, Refund, PaginatedResponse } from '@/api/admin';
import DataTable from '@/components/DataTable';
import StatusTag from '@/components/StatusTag';

const AdminRefunds: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<Refund>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.refunds({ page, page_size: 20, keyword: keyword || undefined, status: status || undefined });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page, keyword, status]);

  useEffect(() => { fetch(); }, [fetch]);

  const process = async (id: string, action: string) => {
    let rejectionReason = '';
    if (action === 'reject') {
      rejectionReason = prompt('拒绝原因：') || '';
      if (!rejectionReason) return;
    }
    try {
      await adminApi.processRefund(id, { action, rejection_reason: rejectionReason });
      Message.success(action === 'approve' ? '已通过' : '已拒绝');
      fetch();
    } catch {
      Message.error('操作失败');
    }
  };

  const columns = [
    { title: '退款单号', dataIndex: 'trade_no', width: 200, ellipsis: true },
    { title: '商户ID', dataIndex: 'merchant_id', width: 220, ellipsis: true },
    { title: '订单ID', dataIndex: 'order_id', width: 220, ellipsis: true },
    { title: '金额', dataIndex: 'amount', width: 100, render: (_: unknown, r: Refund) => `${(r.amount / 100).toFixed(2)}` },
    { title: '原因', dataIndex: 'reason', ellipsis: true },
    { title: '状态', dataIndex: 'status', width: 100, render: (_: unknown, r: Refund) => <StatusTag status={r.status} /> },
    {
      title: '创建时间', dataIndex: 'created_at', width: 180,
      render: (_: unknown, r: Refund) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
    {
      title: '操作', dataIndex: 'id', width: 150,
      render: (id: string, r: Refund) =>
        r.status === 'pending' ? (
          <Space>
            <Button size="small" type="text" status="success" onClick={() => process(id, 'approve')}>通过</Button>
            <Button size="small" type="text" status="danger" onClick={() => process(id, 'reject')}>拒绝</Button>
          </Space>
        ) : null,
    },
  ];

  return (
    <Card title="退款管理">
      <Space style={{ marginBottom: 16 }}>
        <Input prefix={<IconSearch />} placeholder="搜索" value={keyword} onChange={setKeyword} style={{ width: 240 }} allowClear />
        <Select placeholder="状态" value={status || undefined} onChange={(v) => setStatus(v || '')} style={{ width: 120 }} allowClear>
          <Select.Option value="pending">待处理</Select.Option>
          <Select.Option value="completed">已完成</Select.Option>
          <Select.Option value="rejected">已拒绝</Select.Option>
        </Select>
        <Button type="primary" onClick={fetch}>搜索</Button>
      </Space>
      <DataTable columns={columns} data={data} loading={loading} rowKey="id" onChange={(p) => setPage(p.current || 1)} />
    </Card>
  );
};

export default AdminRefunds;
