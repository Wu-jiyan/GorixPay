import { Routes, Route } from 'react-router-dom';
import Landing from './pages/landing/Index';
import Register from './pages/landing/Register';
import Cashier from './pages/cashier/Index';
import NotFound from './pages/NotFound';
import AdminLogin from './pages/login/AdminLogin';
import MerchantLogin from './pages/login/MerchantLogin';
import AdminLayout from './layouts/AdminLayout';
import MerchantLayout from './layouts/MerchantLayout';
import Dashboard from './pages/admin/Dashboard';
import Merchants from './pages/admin/Merchants';
import Orders from './pages/admin/Orders';
import Channels from './pages/admin/Channels';
import AdminRefunds from './pages/admin/Refunds';
import AdminSettlements from './pages/admin/Settlements';
import Ledger from './pages/admin/Ledger';
import Outbox from './pages/admin/Outbox';
import Settings from './pages/admin/Settings';
import AuditLogs from './pages/admin/AuditLogs';
import MerchantOrders from './pages/merchant/Orders';
import MerchantRefunds from './pages/merchant/Refunds';
import MerchantSettlements from './pages/merchant/Settlements';
import Credentials from './pages/merchant/Credentials';

function App() {
  return (
    <Routes>
      {/* 公开页面 */}
      <Route path="/" element={<Landing />} />
      <Route path="/login" element={<AdminLogin />} />
      <Route path="/merchant/login" element={<MerchantLogin />} />
      <Route path="/register" element={<Register />} />
      <Route path="/pay/:tradeNo" element={<Cashier />} />

      {/* 管理端 */}
      <Route path="/admin" element={<AdminLayout />}>
        <Route index element={<Dashboard />} />
        <Route path="merchants" element={<Merchants />} />
        <Route path="orders" element={<Orders />} />
        <Route path="channels" element={<Channels />} />
        <Route path="refunds" element={<AdminRefunds />} />
        <Route path="settlements" element={<AdminSettlements />} />
        <Route path="ledger" element={<Ledger />} />
        <Route path="outbox" element={<Outbox />} />
        <Route path="settings" element={<Settings />} />
        <Route path="audit" element={<AuditLogs />} />
      </Route>

      {/* 商户端 */}
      <Route path="/merchant" element={<MerchantLayout />}>
        <Route index element={<MerchantOrders />} />
        <Route path="orders" element={<MerchantOrders />} />
        <Route path="refunds" element={<MerchantRefunds />} />
        <Route path="settlements" element={<MerchantSettlements />} />
        <Route path="credentials" element={<Credentials />} />
      </Route>

      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}

export default App;
