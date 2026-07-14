import React, { useCallback, useEffect, useState } from 'react';
import { Card, Button, Message, Modal, Form, Input, InputNumber, Space } from '@arco-design/web-react';
import { IconPlus } from '@arco-design/web-react/icon';
import { merchantApi } from '@/api/merchant';
import StatusTag from '@/components/StatusTag';
import { PaginatedResponse } from '@/api/admin';

interface RefundRow {
  id: string;
  order_id: string;
  trade_no: string;
  amount: number;
  status: string;
  reason: string;
  created_at: string;
}

const MerchantRefunds: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<RefundRow>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [modalVisible, setModalVisible] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm();

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await merchantApi.refunds({ page, page_size: 20 });
      setData(res.data);
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => { fetch(); }, [fetch]);

  const onSubmit = async () => {
    try {
      const values = await form.validate();
      setSubmitting(true);
      await merchantApi.createRefund(values);
      Message.success('退款申请已提交');
      setModalVisible(false);
      form.resetFields();
      fetch();
    } catch {
      // validation error
    } finally {
      setSubmitting(false);
    }
  };

  const columns = [
    { title: '退款单号', dataIndex: 'trade_no', width: 200, ellipsis: true },
    { title: '订单ID', dataIndex: 'order_id', width: 200, ellipsis: true },
    { title: '金额', dataIndex: 'amount', width: 100, render: (_: unknown, r: RefundRow) => `${(r.amount / 100).toFixed(2)}` },
    { title: '原因', dataIndex: 'reason', ellipsis: true },
    { title: '状态', dataIndex: 'status', width: 100, render: (_: unknown, r: RefundRow) => <StatusTag status={r.status} /> },
    {
      title: '创建时间', dataIndex: 'created_at', width: 180,
      render: (_: unknown, r: RefundRow) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
  ];

  return (
    <Card
      title="退款管理"
      extra={<Button type="primary" icon={<IconPlus />} onClick={() => setModalVisible(true)}>申请退款</Button>}
    >
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ borderBottom: '1px solid #e5e6eb', textAlign: 'left' }}>
            <th style={{ padding: 8 }}>退款单号</th>
            <th style={{ padding: 8 }}>订单ID</th>
            <th style={{ padding: 8 }}>金额</th>
            <th style={{ padding: 8 }}>原因</th>
            <th style={{ padding: 8 }}>状态</th>
            <th style={{ padding: 8 }}>创建时间</th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr><td colSpan={6} style={{ padding: 16, textAlign: 'center' }}>加载中...</td></tr>
          ) : !data || data.items.length === 0 ? (
            <tr><td colSpan={6} style={{ padding: 16, textAlign: 'center' }}>暂无退款记录</td></tr>
          ) : (
            data.items.map((r) => (
              <tr key={r.id} style={{ borderBottom: '1px solid #f0f0f0' }}>
                <td style={{ padding: 8 }}>{r.trade_no}</td>
                <td style={{ padding: 8 }}>{r.order_id}</td>
                <td style={{ padding: 8 }}>{(r.amount / 100).toFixed(2)}</td>
                <td style={{ padding: 8 }}>{r.reason}</td>
                <td style={{ padding: 8 }}><StatusTag status={r.status} /></td>
                <td style={{ padding: 8 }}>{r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-'}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>

      <Modal
        title="申请退款"
        visible={modalVisible}
        onOk={onSubmit}
        onCancel={() => setModalVisible(false)}
        confirmLoading={submitting}
      >
        <Form form={form} layout="vertical">
          <Form.Item field="order_id" label="订单ID" rules={[{ required: true }]}>
            <Input placeholder="输入要退款的订单ID" />
          </Form.Item>
          <Form.Item field="amount" label="退款金额（分）" rules={[{ required: true }]}>
            <InputNumber min={1} />
          </Form.Item>
          <Form.Item field="reason" label="退款原因" rules={[{ required: true }]}>
            <Input.TextArea placeholder="请输入退款原因" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
};

export default MerchantRefunds;
