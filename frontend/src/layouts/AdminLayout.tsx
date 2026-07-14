import React, { useState } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Menu, Avatar, Button } from '@arco-design/web-react';
import {
  IconDashboard,
  IconUserGroup,
  IconFile,
  IconStorage,
  IconSettings,
  IconSafe,
  IconPenFill,
  IconInteraction,
  IconBook,
  IconNotification,
  IconMenuFold,
  IconMenuUnfold,
  IconExport,
} from '@arco-design/web-react/icon';
import api from '../api/client';

const menuItems = [
  { key: '/admin', icon: <IconDashboard />, label: '仪表盘' },
  { key: '/admin/merchants', icon: <IconUserGroup />, label: '商户管理' },
  { key: '/admin/orders', icon: <IconFile />, label: '订单管理' },
  { key: '/admin/channels', icon: <IconStorage />, label: '渠道管理' },
  { key: '/admin/refunds', icon: <IconPenFill />, label: '退款审核' },
  { key: '/admin/settlements', icon: <IconInteraction />, label: '结算审核' },
  { key: '/admin/ledger', icon: <IconBook />, label: '账本管理' },
  { key: '/admin/outbox', icon: <IconNotification />, label: '通知队列' },
  { key: '/admin/settings', icon: <IconSettings />, label: '系统设置' },
  { key: '/admin/audit', icon: <IconSafe />, label: '审计日志' },
];

const AdminLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(false);

  const currentKey = location.pathname === '/admin' ? '/admin' : '/' + location.pathname.split('/').slice(1, 3).join('/');

  const handleLogout = async () => {
    try {
      await api.post('/auth/logout');
    } finally {
      navigate('/login');
    }
  };

  return (
    <div className="admin-layout">
      <div className={`admin-sidebar${collapsed ? ' collapsed' : ''}`}>
        <div className="sidebar-logo">
          <IconSafe style={{ fontSize: 22, color: '#165dff', flexShrink: 0 }} />
          <span className="logo-text">GorixPay</span>
        </div>
        <div className="sidebar-menu">
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
        <div className="sidebar-footer">
          <Avatar size={collapsed ? 28 : 32} style={{ backgroundColor: '#165dff', flexShrink: 0 }}>
            管
          </Avatar>
          {!collapsed && <span style={{ color: '#c9cdd4' }}>管理员</span>}
        </div>
      </div>
      <div className="admin-main">
        <div className="admin-header">
          <div className="admin-header-left">
            <Button
              type="text"
              icon={collapsed ? <IconMenuUnfold /> : <IconMenuFold />}
              onClick={() => setCollapsed(!collapsed)}
              style={{ fontSize: 18 }}
            />
          </div>
          <div className="admin-header-right">
            <span style={{ color: '#4e5969' }}>管理员</span>
            <Button type="text" icon={<IconExport />} onClick={handleLogout}>
              退出
            </Button>
          </div>
        </div>
        <div className="admin-content">
          <Outlet />
        </div>
      </div>
    </div>
  );
};

export default AdminLayout;
