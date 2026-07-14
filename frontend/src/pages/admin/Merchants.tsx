import React, { useCallback, useEffect, useState } from 'react';
import { Card, Input, Select, Button, Message, Popconfirm, Space } from '@arco-design/web-react';
import { IconSearch } from '@arco-design/web-react/icon';
import { adminApi, Merchant, PaginatedResponse } from '@/api/admin';
import DataTable from '@/components/DataTable';
import StatusTag from '@/components/StatusTag';

const Merchants: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<Merchant>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.merchants({ page, page_size: 20, keyword: keyword || undefined, status: status || undefined });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page, keyword, status]);

  useEffect(() => { fetch(); }, [fetch]);

  const toggleStatus = async (id: string, current: string) => {
    const newStatus = current === 'active' ? 'disabled' : 'active';
    try {
      await adminApi.updateMerchantStatus(id, newStatus);
      Message.success('状态已更新');
      fetch();
    } catch {
      Message.error('操作失败');
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 220, ellipsis: true },
    { title: '名称', dataIndex: 'display_name' },
    { title: '邮箱', dataIndex: 'email' },
    { title: '手机', dataIndex: 'phone' },
    { title: '状态', dataIndex: 'status', render: (_: unknown, r: Merchant) => <StatusTag status={r.status} /> },
    {
      title: '创建时间', dataIndex: 'created_at',
      render: (_: unknown, r: Merchant) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
    {
      title: '操作', dataIndex: 'id',
      render: (id: string, r: Merchant) => (
        <Popconfirm
          title={`确定要${r.status === 'active' ? '禁用' : '启用'}该商户吗？`}
          onOk={() => toggleStatus(id, r.status)}
        >
          <Button size="small" type="text" status={r.status === 'active' ? 'danger' : 'success'}>
            {r.status === 'active' ? '禁用' : '启用'}
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <Card title="商户管理">
      <Space style={{ marginBottom: 16 }}>
        <Input prefix={<IconSearch />} placeholder="搜索名称/邮箱/手机" value={keyword} onChange={setKeyword} style={{ width: 240 }} allowClear />
        <Select placeholder="状态" value={status || undefined} onChange={(v) => setStatus(v || '')} style={{ width: 120 }} allowClear>
          <Select.Option value="active">启用</Select.Option>
          <Select.Option value="disabled">禁用</Select.Option>
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

export default Merchants;
