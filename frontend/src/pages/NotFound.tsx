import React from 'react';
import { useNavigate } from 'react-router-dom';
import { Result, Button } from '@arco-design/web-react';

const NotFound: React.FC = () => {
  const navigate = useNavigate();

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
      <Result
        status="404"
        title="页面不存在"
        subTitle="您访问的页面不存在或已被移除"
        extra={
          <Button type="primary" onClick={() => navigate('/', { replace: true })}>
            返回首页
          </Button>
        }
      />
    </div>
  );
};

export default NotFound;
