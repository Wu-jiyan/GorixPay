import React, { useCallback, useEffect, useState } from 'react';
import { Card } from '@arco-design/web-react';
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

  const columns = [
    { title: '事件类型', dataIndex: 'event_type', width: 120 },
    { title: '操作者类型', dataIndex: 'actor_type', width: 100 },
    { title: '操作者ID', dataIndex: 'actor_id', width: 220, ellipsis: true },
    {
      title: 'Payload', dataIndex: 'payload', width: 300, ellipsis: true,
      render: (_: unknown, r: AuditEvent) => {
        try { return JSON.stringify(r.payload); } catch { return '-'; }
      },
    },
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
