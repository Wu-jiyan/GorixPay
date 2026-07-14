import React from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Menu } from '@arco-design/web-react';
import {
  IconDashboard,
  IconUserGroup,
  IconFile,
  IconStorage,
  IconSettings,
  IconSafe,
  IconPenFill,
  IconInteraction,
} from '@arco-design/web-react/icon';

const Sider = Layout.Sider;
const Header = Layout.Header;
const Content = Layout.Content;

const menuItems = [
  { key: '/admin', icon: <IconDashboard />, label: '仪表盘' },
  { key: '/admin/merchants', icon: <IconUserGroup />, label: '商户管理' },
  { key: '/admin/orders', icon: <IconFile />, label: '订单管理' },
  { key: '/admin/channels', icon: <IconStorage />, label: '渠道管理' },
  { key: '/admin/refunds', icon: <IconPenFill />, label: '退款管理' },
  { key: '/admin/settlements', icon: <IconInteraction />, label: '结算管理' },
  { key: '/admin/settings', icon: <IconSettings />, label: '系统设置' },
  { key: '/admin/audit', icon: <IconSafe />, label: '审计日志' },
];

const AdminLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const currentKey = location.pathname === '/admin' ? '/admin' : '/' + location.pathname.split('/').slice(1, 3).join('/');

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider>
        <div style={{ height: 60, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#fff', fontSize: 18, fontWeight: 'bold' }}>
          GorixPay
        </div>
        <Menu
          selectedKeys={[currentKey]}
          onClickMenuItem={(key) => navigate(key)}
          style={{ flex: 1 }}
        >
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
          <span>Admin</span>
        </Header>
        <Content style={{ padding: 24, background: '#f5f5f5' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default AdminLayout;
