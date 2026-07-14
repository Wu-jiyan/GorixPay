import { Routes, Route, Navigate } from 'react-router-dom';
import AdminLogin from './pages/login/AdminLogin';
import MerchantLogin from './pages/login/MerchantLogin';
import AdminLayout from './layouts/AdminLayout';
import MerchantLayout from './layouts/MerchantLayout';
import Dashboard from './pages/admin/Dashboard';
import Merchants from './pages/admin/Merchants';
import Orders from './pages/admin/Orders';
import Channels from './pages/admin/Channels';
import Settings from './pages/admin/Settings';
import AuditLogs from './pages/admin/AuditLogs';
import AdminRefunds from './pages/admin/Refunds';
import AdminSettlements from './pages/admin/Settlements';
import MerchantOrders from './pages/merchant/Orders';
import MerchantRefunds from './pages/merchant/Refunds';
import MerchantSettlements from './pages/merchant/Settlements';

function App() {
  return (
    <Routes>
      <Route path="/login" element={<AdminLogin />} />
      <Route path="/merchant/login" element={<MerchantLogin />} />
      <Route path="/admin" element={<AdminLayout />}>
        <Route index element={<Dashboard />} />
        <Route path="merchants" element={<Merchants />} />
        <Route path="orders" element={<Orders />} />
        <Route path="channels" element={<Channels />} />
        <Route path="settings" element={<Settings />} />
        <Route path="audit" element={<AuditLogs />} />
        <Route path="refunds" element={<AdminRefunds />} />
        <Route path="settlements" element={<AdminSettlements />} />
      </Route>
      <Route path="/merchant" element={<MerchantLayout />}>
        <Route index element={<MerchantOrders />} />
        <Route path="refunds" element={<MerchantRefunds />} />
        <Route path="settlements" element={<MerchantSettlements />} />
      </Route>
      <Route path="*" element={<Navigate to="/admin" replace />} />
    </Routes>
  );
}

export default App;
