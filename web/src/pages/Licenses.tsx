import { useState } from 'react'
import { Button, Card, Input, Select, Table, Tag } from 'antd'
import { Link } from 'react-router'
import type { LicenseSummary } from '../api'
import { can, useAuth } from '../auth'
import { faNum } from '../format'
import { ExpiryTag, Mono, PageHeader, statusTag } from '../ui'
import { useFetch } from '../useFetch'

export default function Licenses() {
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [expiring, setExpiring] = useState(0)
  const { user } = useAuth()
  const list = useFetch<{ items: LicenseSummary[] }>(`/licenses?q=${encodeURIComponent(q)}&status=${status}&expiring_in=${expiring}`)
  const items = list.data?.items ?? []

  return (
    <>
      <PageHeader
        title="لایسنس‌ها"
        subtitle={<>
          <bdi>{faNum(items.length)} لایسنس</bdi> · <bdi>{faNum(items.reduce((n, l) => n + l.serials, 0))} سریال</bdi> · <bdi>{faNum(items.reduce((n, l) => n + l.otp_users, 0))} کاربر 2FA</bdi>
        </>}
        extra={can(user, 'licenses.write') && <Link to="/licenses/new"><Button type="primary">+ صدور لایسنس</Button></Link>}
      />
      <Card styles={{ body: { padding: 0 } }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, padding: 16 }}>
          <Input.Search allowClear placeholder="شماره‌ی لایسنس، سریال، مشتری یا شناسه‌ی نصب" onSearch={setQ} style={{ maxWidth: 380 }} />
          <Select value={status} onChange={setStatus} style={{ width: 160 }}
            options={[{ value: '', label: 'همه‌ی وضعیت‌ها' }, { value: 'active', label: 'فعال' }, { value: 'revoked', label: 'باطل‌شده' }]} />
          <Select value={expiring} onChange={setExpiring} style={{ width: 180 }}
            options={[{ value: 0, label: 'هر زمان انقضا' }, { value: 30, label: 'انقضا تا ۳۰ روز' }, { value: 90, label: 'انقضا تا ۹۰ روز' }]} />
        </div>
        <Table
          rowKey="id" loading={list.loading} dataSource={items} scroll={{ x: true }}
          columns={[
            { title: 'لایسنس', dataIndex: 'code', render: (v, r) => <Link to={`/licenses/${r.id}`} style={{ fontWeight: 600 }}><Mono>{v}</Mono></Link> },
            { title: 'مشتری', dataIndex: 'customer_name', render: (v, r) => <Link to={`/customers/${r.customer_id}`}>{v}</Link> },
            { title: 'نماینده', dataIndex: 'reseller_name', render: (v) => v || 'مستقیم' },
            { title: 'سریال‌ها', dataIndex: 'serials', render: faNum },
            { title: '2FA', dataIndex: 'otp_users', render: faNum },
            { title: 'نصب', dataIndex: 'installation_id', render: (v) => (v ? <Mono>{String(v).slice(0, 9)}…</Mono> : <Tag>فعال‌نشده</Tag>) },
            { title: 'روش', dataIndex: 'mode', render: (v) => (v === 'online' ? 'آنلاین و آفلاین' : 'فقط آفلاین') },
            { title: 'نزدیک‌ترین انقضا', dataIndex: 'nearest_expiry', render: (v, r) => <ExpiryTag date={v} grace={r.grace_days} /> },
            { title: 'وضعیت', dataIndex: 'status', render: statusTag },
          ]}
        />
      </Card>
    </>
  )
}
