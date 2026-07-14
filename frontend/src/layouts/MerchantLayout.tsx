import React from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Menu } from '@arco-design/web-react';
import { IconFile, IconPenFill, IconInteraction } from '@arco-design/web-react/icon';

const Sider = Layout.Sider;
const Header = Layout.Header;
const Content = Layout.Content;

const menuItems = [
  { key: '/merchant', icon: <IconFile />, label: '订单管理' },
  { key: '/merchant/refunds', icon: <IconPenFill />, label: '退款管理' },
  { key: '/merchant/settlements', icon: <IconInteraction />, label: '结算管理' },
];

const MerchantLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const currentKey = location.pathname === '/merchant' ? '/merchant' : '/' + location.pathname.split('/').slice(1, 3).join('/');

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider>
        <div style={{ height: 60, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#fff', fontSize: 18, fontWeight: 'bold' }}>
          GorixPay
        </div>
        <Menu selectedKeys={[currentKey]} onClickMenuItem={(key) => navigate(key)}>
          {menuItems.map((item) => (
            <Menu.Item key={item.key}>
              {item.icon}
              {item.label}
            </Menu.Item>
          ))}
        </Menu>
      </Sider>
      <Layout>
        <Header style={{ padding: '0 24px', display: 'flex', alignItems: 'center', justifyContent: 'flex-end', background: '#fff', borderBottom: '1px solid #e5e6eb' }}>
          <span>商户中心</span>
        </Header>
        <Content style={{ padding: 24, background: '#f5f5f5' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MerchantLayout;
