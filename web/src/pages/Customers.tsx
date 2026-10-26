import { useState } from 'react'
import { App, Button, Card, Input, Table } from 'antd'
import { Link, useNavigate } from 'react-router'
import { api, type Org } from '../api'
import { can, useAuth } from '../auth'
import { faNum } from '../format'
import { ExpiryTag, Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'
import OrgForm, { type OrgFormValues } from './OrgForm'

export default function Customers() {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const { user } = useAuth()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const list = useFetch<{ items: Org[] }>('/customers?q=' + encodeURIComponent(q))

  const create = async (v: OrgFormValues) => {
    setBusy(true)
    try {
      const o = await api.post<Org>('/customers', v)
      message.success('مشتری ساخته شد')
      navigate(`/customers/${o.id}`)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title="مشتریان" subtitle={`${faNum(list.data?.items.length ?? 0)} مشتری`}
        extra={can(user, 'customers.write') && <Button type="primary" onClick={() => setOpen(true)}>+ مشتری جدید</Button>}
      />
      <Card styles={{ body: { padding: 0 } }}>
        <div style={{ padding: 16 }}>
          <Input.Search allowClear placeholder="جستجوی نام، کد یا شناسه‌ی ملی" onSearch={setQ} style={{ maxWidth: 360 }} />
        </div>
        <Table
          rowKey="id" loading={list.loading} dataSource={list.data?.items ?? []} scroll={{ x: true }}
          columns={[
            { title: 'کد', dataIndex: 'code', render: (v) => <Mono>{v}</Mono> },
            { title: 'نام', dataIndex: 'name', render: (v, r) => <Link to={`/customers/${r.id}`} style={{ fontWeight: 600 }}>{v}</Link> },
            { title: 'نماینده', dataIndex: 'parent_name', render: (v) => v || 'مستقیم' },
            { title: 'لایسنس‌ها', dataIndex: 'licenses', render: faNum },
            { title: 'سریال‌ها', dataIndex: 'serials', render: faNum },
            { title: 'کاربران 2FA', dataIndex: 'otp_users', render: faNum },
            { title: 'نزدیک‌ترین انقضا', dataIndex: 'nearest_expiry', render: (v) => <ExpiryTag date={v} /> },
          ]}
        />
      </Card>
      <OrgForm kind="customer" open={open} busy={busy} onCancel={() => setOpen(false)} onSubmit={create} />
    </>
  )
}
