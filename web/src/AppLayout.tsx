import { useState } from 'react'
import { Avatar, Button, Drawer, Grid, Layout, Menu, Typography } from 'antd'
import { Link, Outlet, useLocation } from 'react-router'
import { useAuth, can } from './auth'
import { brand } from './theme'

const { Sider, Header, Content } = Layout

const roleNames: Record<string, string> = {
  owner: 'مالک', admin: 'مدیر', support: 'پشتیبانی', publisher: 'انتشار پکیج', viewer: 'مشاهده‌گر',
}

function NavMenu({ onNavigate }: { onNavigate?: () => void }) {
  const { user } = useAuth()
  const { pathname } = useLocation()
  const key = '/' + (pathname.split('/')[1] ?? '')
  const link = (to: string, label: string) => ({ key: to, label: <Link to={to} onClick={onNavigate}>{label}</Link> })
  const items = [
    link('/', 'داشبورد'),
    {
      type: 'group' as const, label: 'مشتریان و لایسنس',
      children: [
        link('/customers', 'مشتریان'),
        link('/licenses', 'لایسنس‌ها'),
        ...(can(user, 'activation.offline') ? [link('/offline', 'فعال‌سازی آفلاین')] : []),
        link('/installations', 'نصب‌ها'),
      ],
    },
    { type: 'group' as const, label: 'فروش', children: [link('/resellers', 'نمایندگان')] },
    { type: 'group' as const, label: 'مخزن', children: [link('/packages', 'پکیج‌ها و فرم‌ور')] },
    ...(can(user, 'audit.read') ? [{ type: 'group' as const, label: 'سیستم', children: [link('/audit', 'لاگ ممیزی')] }] : []),
  ]
  return <Menu mode="inline" selectedKeys={[key === '/licenses' && pathname === '/licenses/new' ? '/licenses' : key]} items={items} style={{ borderInlineEnd: 0 }} />
}

function Brand() {
  return (
    <Link to="/" style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4, padding: '20px 16px 12px' }}>
      <img src="/logo.png" alt="TaktaPlus" style={{ width: 150, height: 'auto' }} />
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>سرور لایسنس</Typography.Text>
    </Link>
  )
}

export default function AppLayout() {
  const { user, logout } = useAuth()
  const screens = Grid.useBreakpoint()
  const [open, setOpen] = useState(false)
  const mobile = !screens.lg

  return (
    <Layout style={{ minHeight: '100vh' }}>
      {!mobile && (
        <Sider width={240} style={{ borderInlineStart: '1px solid #EEE' }}>
          <Brand />
          <NavMenu />
        </Sider>
      )}
      <Drawer open={mobile && open} onClose={() => setOpen(false)} placement="right" size={260} styles={{ body: { padding: 0 } }}>
        <Brand />
        <NavMenu onNavigate={() => setOpen(false)} />
      </Drawer>
      <Layout>
        <Header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, paddingInline: 24, borderBottom: '1px solid #EEE' }}>
          {mobile ? <Button onClick={() => setOpen(true)}>منو</Button> : <span />}
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <Avatar style={{ background: brand.primary }}>{user?.name.slice(0, 1)}</Avatar>
            <div style={{ lineHeight: 1.4 }}>
              <div style={{ fontWeight: 600 }}>{user?.name}</div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>{roleNames[user?.role ?? ''] ?? user?.role}</Typography.Text>
            </div>
            <Button type="link" onClick={() => void logout()}>خروج</Button>
          </div>
        </Header>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
