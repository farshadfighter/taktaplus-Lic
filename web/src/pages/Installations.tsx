import { useState } from 'react'
import { Card, Input, Table, Tag } from 'antd'
import { Link } from 'react-router'
import type { InstallationRow } from '../api'
import { faDateTime, faNum } from '../format'
import { Mono, PageHeader, statusTag, warningLabels } from '../ui'
import { useFetch } from '../useFetch'

export default function Installations() {
  const [q, setQ] = useState('')
  const list = useFetch<{ items: InstallationRow[] }>('/installations?q=' + encodeURIComponent(q))
  return (
    <>
      <PageHeader title="نصب‌ها" subtitle="هر نصب TaktaPlus با کلید خودش شناخته می‌شود · نصب‌های آفلاین Heartbeat ندارند" />
      <Card styles={{ body: { padding: 0 } }}>
        <div style={{ padding: 16 }}><Input.Search allowClear placeholder="شناسه‌ی نصب، مشتری یا لایسنس" onSearch={setQ} style={{ maxWidth: 360 }} /></div>
        <Table
          rowKey="id" loading={list.loading} dataSource={list.data?.items ?? []} scroll={{ x: true }}
          columns={[
            { title: 'شناسه‌ی نصب', dataIndex: 'id', render: (v) => <Mono>{v}</Mono> },
            { title: 'مشتری / لایسنس', key: 'lic', render: (_, r) => (r.license_id ? <>{r.customer_name} · <Link to={`/licenses/${r.license_id}`}><Mono>{r.license_code}</Mono></Link></> : '—') },
            { title: 'نسخه', dataIndex: 'product_version', render: (v) => <Mono>{v || '—'}</Mono> },
            { title: 'روش', dataIndex: 'mode', render: (v) => (v === 'online' ? 'آنلاین' : v === 'offline' ? 'آفلاین' : '—') },
            { title: 'آخرین ارتباط', dataIndex: 'last_heartbeat_at', render: faDateTime },
            { title: 'مصرف', dataIndex: 'last_usage', render: (u) => `${faNum(u.devices ?? 0)} دستگاه · ${faNum(u.otp_users ?? 0)} کاربر` },
            { title: 'هشدار', dataIndex: 'warnings', render: (w: string[]) => (w.length ? w.map((x) => <Tag key={x} color="red">{warningLabels[x] ?? x}</Tag>) : <Tag color="green">—</Tag>) },
            { title: 'وضعیت', dataIndex: 'status', render: statusTag },
          ]}
        />
      </Card>
    </>
  )
}
