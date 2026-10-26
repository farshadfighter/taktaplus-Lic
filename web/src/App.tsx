import { Spin } from 'antd'
import { Navigate, Route, Routes } from 'react-router'
import { AuthProvider, useAuth } from './auth'
import AppLayout from './AppLayout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Customers from './pages/Customers'
import CustomerDetail from './pages/CustomerDetail'
import Resellers from './pages/Resellers'
import Licenses from './pages/Licenses'
import LicenseNew from './pages/LicenseNew'
import LicenseDetail from './pages/LicenseDetail'
import Offline from './pages/Offline'
import Installations from './pages/Installations'
import Audit from './pages/Audit'
import Packages from './pages/Packages'

function Protected() {
  const { user, loading } = useAuth()
  if (loading) return <Spin fullscreen />
  if (!user) return <Navigate to="/login" replace />
  return <AppLayout />
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<Protected />}>
          <Route index element={<Dashboard />} />
          <Route path="customers" element={<Customers />} />
          <Route path="customers/:id" element={<CustomerDetail />} />
          <Route path="resellers" element={<Resellers />} />
          <Route path="licenses" element={<Licenses />} />
          <Route path="licenses/new" element={<LicenseNew />} />
          <Route path="licenses/:id" element={<LicenseDetail />} />
          <Route path="offline" element={<Offline />} />
          <Route path="installations" element={<Installations />} />
          <Route path="audit" element={<Audit />} />
          <Route path="packages" element={<Packages />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
