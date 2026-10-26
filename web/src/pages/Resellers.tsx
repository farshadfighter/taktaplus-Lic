import { useState } from 'react'
import { App, Button, Card, Table } from 'antd'
import { api, type Org } from '../api'
import { can, useAuth } from '../auth'
import { faDate } from '../format'
import { Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'
import OrgForm, { type OrgFormValues } from './OrgForm'

const tiers: Record<string, string> = { gold: 'طلایی', silver: 'نقره‌ای', bronze: 'برنزی' }

export default function Resellers() {
  const [edit, setEdit] = useState<Org | null | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const { user } = useAuth()
  const { message } = App.useApp()
  const list = useFetch<{ items: Org[] }>('/resellers')

  const save = async (v: OrgFormValues) => {
    setBusy(true)
    try {
      if (edit) await api.put(`/resellers/${edit.id}`, v)
      else await api.post('/resellers', v)
      setEdit(undefined)
      void list.reload()
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const writable = can(user, 'resellers.write')

  return (
    <>
      <PageHeader
        title="نمایندگان فروش" subtitle="اعتبار پیش‌خرید و پورتال نمایندگان در فاز ۵ اضافه می‌شود"
        extra={writable && <Button type="primary" onClick={() => setEdit(null)}>+ نماینده</Button>}
      />
      <Card styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id" loading={list.loading} dataSource={list.data?.items ?? []} scroll={{ x: true }}
          columns={[
            { title: 'کد', dataIndex: 'code', render: (v) => <Mono>{v}</Mono> },
            { title: 'نام', dataIndex: 'name', render: (v) => <b>{v}</b> },
            { title: 'سطح', dataIndex: 'tier', render: (v) => tiers[v] ?? '—' },
            { title: 'رابط', dataIndex: ['contact', 'tech_name'], render: (v) => v || '—' },
            { title: 'از', dataIndex: 'created_at', render: faDate },
            ...(writable ? [{ title: '', key: 'edit', render: (_: unknown, r: Org) => <Button type="link" onClick={() => setEdit(r)}>ویرایش</Button> }] : []),
          ]}
        />
      </Card>
      <OrgForm kind="reseller" open={edit !== undefined} initial={edit} busy={busy} onCancel={() => setEdit(undefined)} onSubmit={save} />
    </>
  )
}
