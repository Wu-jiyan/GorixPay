import React from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Menu, Button } from '@arco-design/web-react';
import {
  IconFile,
  IconPenFill,
  IconInteraction,
  IconCode,
  IconExport,
  IconSafe,
} from '@arco-design/web-react/icon';
import api from '../api/client';

const navItems = [
  { key: '/merchant', label: '概览' },
  { key: '/merchant/orders', label: '订单管理' },
  { key: '/merchant/refunds', label: '退款管理' },
  { key: '/merchant/settlements', label: '结算管理' },
  { key: '/merchant/api-keys', label: 'API 密钥' },
];

const menuItems = [
  { key: '/merchant', icon: <IconFile />, label: '概览' },
  { key: '/merchant/orders', icon: <IconFile />, label: '订单管理' },
  { key: '/merchant/refunds', icon: <IconPenFill />, label: '退款管理' },
  { key: '/merchant/settlements', icon: <IconInteraction />, label: '结算管理' },
  { key: '/merchant/api-keys', icon: <IconCode />, label: 'API 密钥' },
];

const MerchantLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const currentKey = location.pathname === '/merchant' ? '/merchant' : '/' + location.pathname.split('/').slice(1, 3).join('/');

  const handleLogout = async () => {
    try {
      await api.post('/auth/logout');
    } finally {
      navigate('/merchant/login');
    }
  };

  return (
    <div className="merchant-layout">
      <div className="merchant-header">
        <div className="merchant-header-left">
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 18, fontWeight: 700, color: '#1d2129' }}>
            <IconSafe style={{ fontSize: 22, color: '#165dff' }} />
            GorixPay
          </span>
          <div style={{ display: 'flex', gap: 4 }}>
            {navItems.map((item) => (
              <Button
                key={item.key}
                type={currentKey === item.key ? 'primary' : 'text'}
                size="small"
                onClick={() => navigate(item.key)}
              >
                {item.label}
              </Button>
            ))}
          </div>
        </div>
        <div className="merchant-header-right">
          <span style={{ color: '#4e5969' }}>商户中心</span>
          <Button type="text" icon={<IconExport />} onClick={handleLogout}>
            退出
          </Button>
        </div>
      </div>
      <div className="merchant-body">
        <div className="merchant-sidebar">
          <Menu
            selectedKeys={[currentKey]}
            onClickMenuItem={(key) => navigate(key)}
          >
            {menuItems.map((item) => (
              <Menu.Item key={item.key}>
                {item.icon}
                {item.label}
              </Menu.Item>
            ))}
          </Menu>
        </div>
        <div className="merchant-content">
          <Outlet />
        </div>
      </div>
    </div>
  );
};

export default MerchantLayout;
