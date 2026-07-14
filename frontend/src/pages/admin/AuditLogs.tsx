import React, { useCallback, useEffect, useState } from 'react';
import { Card, Tag } from '@arco-design/web-react';
import { adminApi, AuditEvent, PaginatedResponse } from '@/api/admin';
import DataTable from '@/components/DataTable';

const AuditLogs: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<AuditEvent>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.auditEvents({ page, page_size: 20 });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => { fetch(); }, [fetch]);

  const methodColors: Record<string, string> = { GET: 'blue', POST: 'green', PUT: 'orange', PATCH: 'purple', DELETE: 'red' };

  const columns = [
    {
      title: '方法', dataIndex: 'method', width: 80,
      render: (_: unknown, r: AuditEvent) => <Tag color={methodColors[r.method] || 'gray'}>{r.method}</Tag>,
    },
    { title: '路径', dataIndex: 'path', ellipsis: true },
    { title: '状态码', dataIndex: 'status', width: 80 },
    { title: '资源ID', dataIndex: 'resource_id', width: 220, ellipsis: true },
    { title: '操作者', dataIndex: 'principal_id', width: 220, ellipsis: true },
    {
      title: '时间', dataIndex: 'created_at', width: 180,
      render: (_: unknown, r: AuditEvent) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
  ];

  return (
    <Card title="审计日志">
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

export default AuditLogs;
