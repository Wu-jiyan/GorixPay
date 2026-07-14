import React from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@arco-design/web-react';
import { IconThunderbolt, IconSafe, IconSync } from '@arco-design/web-react/icon';

const features = [
  {
    icon: <IconThunderbolt style={{ fontSize: 40, color: '#165dff' }} />,
    title: '多渠道聚合',
    desc: '统一对接微信、支付宝等多家支付渠道，一套 API 搞定所有支付',
  },
  {
    icon: <IconSync style={{ fontSize: 40, color: '#165dff' }} />,
    title: '实时对账',
    desc: '订单状态实时同步，资金流水清晰可追溯，每日自动对账',
  },
  {
    icon: <IconSafe style={{ fontSize: 40, color: '#165dff' }} />,
    title: '安全可靠',
    desc: '银行级数据加密，资金账户独立隔离，交易全程风控保护',
  },
];

const Landing: React.FC = () => {
  return (
    <div>
      <nav className="landing-nav">
        <div className="landing-nav-inner">
          <span className="landing-logo">GorixPay</span>
          <div className="landing-nav-links">
            <Link to="/merchant/login">
              <Button type="outline">商户登录</Button>
            </Link>
            <Link to="/login">
              <Button type="outline">管理登录</Button>
            </Link>
          </div>
        </div>
      </nav>

      <section className="landing-hero">
        <h1>GorixPay — 简单可靠的支付网关</h1>
        <p>对接多家支付渠道，一站式管理订单、退款和结算。开放 API，分钟级集成。</p>
        <div className="landing-hero-actions">
          <Link to="/register">
            <Button type="primary" size="large">免费注册</Button>
          </Link>
          <Link to="/merchant/login">
            <Button size="large">商户登录</Button>
          </Link>
        </div>
      </section>

      <section className="landing-features">
        {features.map((f, i) => (
          <div className="landing-feature" key={i}>
            <div style={{ marginBottom: 16 }}>{f.icon}</div>
            <h3>{f.title}</h3>
            <p>{f.desc}</p>
          </div>
        ))}
      </section>

      <footer className="landing-footer">
        &copy; 2026 GorixPay. All rights reserved.
      </footer>
    </div>
  );
};

export default Landing;
