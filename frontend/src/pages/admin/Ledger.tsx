import React, { useCallback, useEffect, useState } from 'react';
import { Card, Tabs } from '@arco-design/web-react';
import { adminApi, LedgerAccount, LedgerTransaction, LedgerOperation, PaginatedResponse } from '@/api/admin';
import DataTable from '@/components/DataTable';

const TabPane = Tabs.TabPane;

const accountsColumns = [
  { title: '账户ID', dataIndex: 'id', width: 200, ellipsis: true },
  { title: '商户ID', dataIndex: 'merchant_id', width: 200, ellipsis: true },
  { title: '类型', dataIndex: 'type' },
  {
    title: '余额',
    dataIndex: 'balance',
    render: (_: unknown, r: LedgerAccount) => `${(r.balance / 100).toFixed(2)} 元`,
  },
  {
    title: '冻结金额',
    dataIndex: 'frozen',
    render: (_: unknown, r: LedgerAccount) => `${(r.frozen / 100).toFixed(2)} 元`,
  },
  {
    title: '创建时间',
    dataIndex: 'created_at',
    render: (_: unknown, r: LedgerAccount) => (r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-'),
  },
];

const transactionsColumns = [
  { title: '交易ID', dataIndex: 'id', width: 200, ellipsis: true },
  { title: '订单号', dataIndex: 'order_no', width: 180 },
  {
    title: '金额',
    dataIndex: 'amount',
    render: (_: unknown, r: LedgerTransaction) => `${(r.amount / 100).toFixed(2)} 元`,
  },
  { title: '类型', dataIndex: 'type' },
  {
    title: '时间',
    dataIndex: 'created_at',
    render: (_: unknown, r: LedgerTransaction) => (r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-'),
  },
];

const operationsColumns = [
  { title: '操作ID', dataIndex: 'id', width: 200, ellipsis: true },
  { title: '商户ID', dataIndex: 'merchant_id', width: 200, ellipsis: true },
  { title: '操作类型', dataIndex: 'type' },
  {
    title: '金额',
    dataIndex: 'amount',
    render: (_: unknown, r: LedgerOperation) => `${(r.amount / 100).toFixed(2)} 元`,
  },
  {
    title: '时间',
    dataIndex: 'created_at',
    render: (_: unknown, r: LedgerOperation) => (r.created_at ? new Date(r.created_at).toLocaleString('zh-CN') : '-'),
  },
];

const Ledger: React.FC = () => {
  const [accounts, setAccounts] = useState<PaginatedResponse<LedgerAccount>>();
  const [transactions, setTransactions] = useState<PaginatedResponse<LedgerTransaction>>();
  const [operations, setOperations] = useState<PaginatedResponse<LedgerOperation>>();
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [activeTab, setActiveTab] = useState('accounts');

  const fetchAccounts = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.getLedgerAccounts({ page, page_size: 20 });
      setAccounts(res.data);
    } finally {
      setLoading(false);
    }
  }, [page]);

  const fetchTransactions = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.getLedgerTransactions({ page, page_size: 20 });
      setTransactions(res.data);
    } finally {
      setLoading(false);
    }
  }, [page]);

  const fetchOperations = useCallback(async () => {
    setLoading(true);
    try {
      const res = await adminApi.getLedgerOperations({ page, page_size: 20 });
      setOperations(res.data);
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    setPage(1);
    if (activeTab === 'accounts') fetchAccounts();
    else if (activeTab === 'transactions') fetchTransactions();
    else fetchOperations();
  }, [activeTab]);

  useEffect(() => {
    if (activeTab === 'accounts') fetchAccounts();
    else if (activeTab === 'transactions') fetchTransactions();
    else fetchOperations();
  }, [page]);

  return (
    <Card title="账本管理">
      <Tabs activeTab={activeTab} onChange={setActiveTab}>
        <TabPane key="accounts" title="账户列表">
          <DataTable
            columns={accountsColumns}
            data={accounts}
            loading={loading}
            rowKey="id"
            onChange={(p) => setPage(p.current || 1)}
          />
        </TabPane>
        <TabPane key="transactions" title="交易记录">
          <DataTable
            columns={transactionsColumns}
            data={transactions}
            loading={loading}
            rowKey="id"
            onChange={(p) => setPage(p.current || 1)}
          />
        </TabPane>
        <TabPane key="operations" title="资金操作">
          <DataTable
            columns={operationsColumns}
            data={operations}
            loading={loading}
            rowKey="id"
            onChange={(p) => setPage(p.current || 1)}
          />
        </TabPane>
      </Tabs>
    </Card>
  );
};

export default Ledger;
