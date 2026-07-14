import React, { useEffect, useState, useCallback } from 'react';
import { Card, Descriptions, Button, Message, Modal, Spin } from '@arco-design/web-react';
import { merchantApi, Credential } from '@/api/merchant';

const Credentials: React.FC = () => {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [loading, setLoading] = useState(true);
  const [rotating, setRotating] = useState(false);

  const fetchCredentials = useCallback(async () => {
    setLoading(true);
    try {
      const res = await merchantApi.getCredentials();
      setCredentials(res.data);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchCredentials();
  }, [fetchCredentials]);

  const handleRotate = (id: string) => {
    Modal.confirm({
      title: '确认轮换',
      content: '轮换密钥后，旧密钥将立即失效，使用旧密钥的 API 调用将失败。确认继续？',
      onOk: async () => {
        setRotating(true);
        try {
          await merchantApi.rotateKey(id);
          Message.success('密钥已轮换');
          await fetchCredentials();
        } catch {
          Message.error('轮换密钥失败');
        } finally {
          setRotating(false);
        }
      },
    });
  };

  if (loading) {
    return <Spin loading style={{ display: 'flex', justifyContent: 'center', marginTop: 100 }} />;
  }

  return (
    <Card title="API 密钥">
      {credentials.length === 0 ? (
        <p style={{ textAlign: 'center', color: '#999', padding: 40 }}>暂无凭据信息</p>
      ) : (
        credentials.map((cred) => (
          <Card key={cred.id} style={{ marginBottom: 16 }} bordered>
            <Descriptions
              data={[
                { label: '凭证 ID', value: cred.id },
                { label: '商户 ID', value: cred.merchant_id },
                {
                  label: '公钥',
                  value: cred.public_key
                    ? cred.public_key.substring(0, 20) + '...'
                    : '-',
                },
                {
                  label: '创建时间',
                  value: cred.created_at ? new Date(cred.created_at).toLocaleString('zh-CN') : '-',
                },
              ]}
              column={2}
              style={{ marginBottom: 16 }}
            />
            <Button
              type="primary"
              status="warning"
              loading={rotating}
              onClick={() => handleRotate(cred.id)}
            >
              轮换密钥
            </Button>
          </Card>
        ))
      )}
    </Card>
  );
};

export default Credentials;
