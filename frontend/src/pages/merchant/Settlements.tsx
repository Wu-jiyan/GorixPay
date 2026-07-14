import React, { useCallback, useEffect, useState } from 'react';
import { Card, Button, Message, Modal, Form, Input, InputNumber, Space } from '@arco-design/web-react';
import { IconPlus } from '@arco-design/web-react/icon';
import { merchantApi } from '@/api/merchant';
import StatusTag from '@/components/StatusTag';
import { PaginatedResponse } from '@/api/admin';

interface SettlementRow {
  id: string;
  amount: number;
  status: string;
  bank_account: string;
  bank_name: string;
  created_at: string;
}

const MerchantSettlements: React.FC = () => {
  const [data, setData] = useState<PaginatedResponse<SettlementRow>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [modalVisible, setModalVisible] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm();

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await merchantApi.settlements({ page, page_size: 20 });
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
      await merchantApi.createSettlement(values);
      Message.success('结算申请已提交');
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
    { title: '结算单号', dataIndex: 'id', width: 220, ellipsis: true },
    { title: '金额', dataIndex: 'amount', width: 100, render: (_: unknown, r: SettlementRow) => `${(r.amount / 100).toFixed(2)}` },
    { title: '银行账户', dataIndex: 'bank_account' },
    { title: '银行名称', dataIndex: 'bank_name' },
    { title: '状态', dataIndex: 'status', width: 100, render: (_: unknown, r: SettlementRow) => <StatusTag status={r.status} /> },
    {
      title: '创建时间', dataIndex: 'created_at', width: 180,
      render: (_: unknown, r: SettlementRow) => r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-',
    },
  ];

  return (
    <Card
      title="结算管理"
      extra={<Button type="primary" icon={<IconPlus />} onClick={() => setModalVisible(true)}>申请结算</Button>}
    >
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ borderBottom: '1px solid #e5e6eb', textAlign: 'left' }}>
            <th style={{ padding: 8 }}>结算单号</th>
            <th style={{ padding: 8 }}>金额</th>
            <th style={{ padding: 8 }}>银行账户</th>
            <th style={{ padding: 8 }}>银行名称</th>
            <th style={{ padding: 8 }}>状态</th>
            <th style={{ padding: 8 }}>创建时间</th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr><td colSpan={6} style={{ padding: 16, textAlign: 'center' }}>加载中...</td></tr>
          ) : !data || data.items.length === 0 ? (
            <tr><td colSpan={6} style={{ padding: 16, textAlign: 'center' }}>暂无结算记录</td></tr>
          ) : (
            data.items.map((s) => (
              <tr key={s.id} style={{ borderBottom: '1px solid #f0f0f0' }}>
                <td style={{ padding: 8 }}>{s.id}</td>
                <td style={{ padding: 8 }}>{(s.amount / 100).toFixed(2)}</td>
                <td style={{ padding: 8 }}>{s.bank_account}</td>
                <td style={{ padding: 8 }}>{s.bank_name}</td>
                <td style={{ padding: 8 }}><StatusTag status={s.status} /></td>
                <td style={{ padding: 8 }}>{s.created_at ? new Date(s.created_at).toLocaleString('zh-CN') : '-'}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>

      <Modal
        title="申请结算"
        visible={modalVisible}
        onOk={onSubmit}
        onCancel={() => setModalVisible(false)}
        confirmLoading={submitting}
      >
        <Form form={form} layout="vertical">
          <Form.Item field="amount" label="结算金额（分）" rules={[{ required: true }]}>
            <InputNumber min={1} />
          </Form.Item>
          <Form.Item field="bank_account" label="银行账号" rules={[{ required: true }]}>
            <Input placeholder="请输入银行账号" />
          </Form.Item>
          <Form.Item field="bank_name" label="开户行" rules={[{ required: true }]}>
            <Input placeholder="请输入开户行名称" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
};

export default MerchantSettlements;
