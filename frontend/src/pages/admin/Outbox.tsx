import React, { useCallback, useEffect, useState } from 'react';
import { Card, Grid, Statistic } from '@arco-design/web-react';
import { adminApi, OutboxStats, OutboxMessage, PaginatedResponse } from '@/api/admin';
import DataTable from '@/components/DataTable';

const Row = Grid.Row;
const Col = Grid.Col;

const columns = [
  { title: '消息ID', dataIndex: 'id', width: 200, ellipsis: true },
  { title: 'Topic', dataIndex: 'topic', width: 180 },
  { title: 'Payload', dataIndex: 'payload', ellipsis: true, width: 200 },
  { title: '状态', dataIndex: 'status' },
  { title: '重试次数', dataIndex: 'retry_count' },
  {
    title: '创建时间',
    dataIndex: 'created_at',
    width: 180,
    render: (_: unknown, r: OutboxMessage) => (r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-'),
  },
  {
    title: '下次重试时间',
    dataIndex: 'next_retry_at',
    width: 180,
    render: (_: unknown, r: OutboxMessage) => (r.next_retry_at ? new Date(r.next_retry_at).toLocaleString('zh-CN') : '-'),
  },
];

const Outbox: React.FC = () => {
  const [stats, setStats] = useState<OutboxStats | null>(null);
  const [data, setData] = useState<PaginatedResponse<OutboxMessage>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);

  const fetchStats = useCallback(async () => {
    try {
      const res = await adminApi.getOutboxStats();
      setStats(res.data);
    } catch {
      // ignore
    }
  }, []);

  const fetchMessages = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.getOutboxMessages({ page, page_size: 20 });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  useEffect(() => {
    fetchMessages();
  }, [fetchMessages]);

  return (
    <div>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="待发送" value={stats?.pending || 0} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="处理中" value={stats?.processing || 0} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="已完成" value={stats?.completed || 0} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="死信" value={stats?.dead || 0} />
          </Card>
        </Col>
      </Row>
      <Card title="消息列表">
        <DataTable
          columns={columns}
          data={data}
          loading={loading}
          rowKey="id"
          onChange={(p) => setPage(p.current || 1)}
        />
      </Card>
    </div>
  );
};

export default Outbox;
