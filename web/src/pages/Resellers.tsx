import { useState } from 'react'
import { App, Button, Card, Drawer, Form, Input, InputNumber, Space, Table, Tag, Typography } from 'antd'
import { api, type CreditTx, type Credits, type Org } from '../api'
import { can, useAuth } from '../auth'
import { faDate, faDateTime, faNum } from '../format'
import OrgUsers from '../OrgUsers'
import { Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'
import OrgForm, { type OrgFormValues } from './OrgForm'

const tiers: Record<string, string> = { gold: 'طلایی', silver: 'نقره‌ای', bronze: 'برنزی' }

const delta = (n: number) => (n === 0 ? '—' : <span dir="ltr" style={{ color: n > 0 ? '#2E7D32' : '#B42318' }}>{n > 0 ? '+' : '−'}{faNum(Math.abs(n))}</span>)

// CreditDrawer allocates prepaid credit to a reseller and lists its
// transactions and portal users.
function CreditDrawer({ org, onClose, onChanged }: { org: Org | null; onClose: () => void; onChanged: () => void }) {
  const { message } = App.useApp()
  const { user } = useAuth()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const data = useFetch<{ balance: Credits; transactions: CreditTx[] }>(org ? `/resellers/${org.id}/credits` : null)
  const writable = can(user, 'resellers.write')
  return (
    <Drawer open={org !== null} onClose={onClose} title={org && `${org.name} · اعتبار و کاربران`} size={640} destroyOnHidden>
      {data.data && (
        <Space size={32} style={{ marginBottom: 16 }}>
          <div><Typography.Text type="secondary">اعتبار سریال‌سال</Typography.Text><div style={{ fontSize: 24, fontWeight: 700 }}>{faNum(data.data.balance.serial_years)}</div></div>
          <div><Typography.Text type="secondary">اعتبار کاربرسال 2FA</Typography.Text><div style={{ fontSize: 24, fontWeight: 700 }}>{faNum(data.data.balance.user_years)}</div></div>
        </Space>
      )}
      {writable && (
        <Card size="small" title="افزودن اعتبار" style={{ marginBottom: 16 }}>
          <Form form={form} layout="inline" requiredMark={false} initialValues={{ serial_years: 0, user_years: 0 }} onFinish={async (v) => {
            setBusy(true)
            try {
              await api.post(`/resellers/${org!.id}/credits`, v)
              message.success('اعتبار ثبت شد')
              form.resetFields()
              void data.reload()
              onChanged()
            } catch (e) {
              message.error((e as Error).message)
            } finally {
              setBusy(false)
            }
          }}>
            <Form.Item name="serial_years" label="سریال‌سال"><InputNumber style={{ width: 90 }} /></Form.Item>
            <Form.Item name="user_years" label="کاربرسال 2FA"><InputNumber style={{ width: 100 }} /></Form.Item>
            <Form.Item name="description" rules={[{ required: true, message: 'شماره‌ی فاکتور یا توضیح' }]} style={{ marginTop: 8 }}><Input placeholder="شماره‌ی فاکتور / توضیح" style={{ width: 240 }} /></Form.Item>
            <Form.Item style={{ marginTop: 8 }}><Button type="primary" htmlType="submit" loading={busy}>افزودن اعتبار</Button></Form.Item>
          </Form>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>برای اصلاح، عدد منفی وارد کنید. هر سریال برای یک سال یک سریال‌سال و هر کاربر 2FA برای یک سال یک کاربرسال مصرف می‌کند.</Typography.Text>
        </Card>
      )}
      <Card size="small" title="تراکنش‌ها" styles={{ body: { padding: 0 } }} style={{ marginBottom: 16 }}>
        <Table<CreditTx> size="small" rowKey="id" pagination={false} dataSource={data.data?.transactions ?? []} locale={{ emptyText: 'تراکنشی نیست' }}
          columns={[
            { title: 'زمان', dataIndex: 'at', render: (v) => faDateTime(v) },
            { title: 'شرح', dataIndex: 'description' },
            { title: 'سریال‌سال', dataIndex: 'serial_years', render: delta },
            { title: 'کاربرسال', dataIndex: 'user_years', render: delta },
            { title: 'مانده', render: (_, t) => `${faNum(t.balance_serial)} / ${faNum(t.balance_user)}` },
          ]} />
      </Card>
      {org && <Card size="small" title="کاربران پورتال نماینده" styles={{ body: { padding: 0 } }}><OrgUsers base={`/orgs/${org.id}/users`} writable={writable} /></Card>}
    </Drawer>
  )
}

export default function Resellers() {
  const [edit, setEdit] = useState<Org | null | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const { user } = useAuth()
  const { message } = App.useApp()
  const list = useFetch<{ items: Org[] }>('/resellers')
  const credits = useFetch<{ items: Record<string, Credits> }>('/resellers/credits')
  const [creditOrg, setCreditOrg] = useState<Org | null>(null)

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
        title="نمایندگان فروش" subtitle="نماینده از اعتبار پیش‌خرید برای مشتریانش لایسنس صادر یا تمدید می‌کند"
        extra={writable && <Button type="primary" onClick={() => setEdit(null)}>+ نماینده</Button>}
      />
      <Card styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id" loading={list.loading} dataSource={list.data?.items ?? []} scroll={{ x: true }}
          columns={[
            { title: 'کد', dataIndex: 'code', render: (v) => <Mono>{v}</Mono> },
            { title: 'نام', dataIndex: 'name', render: (v) => <b>{v}</b> },
            { title: 'سطح', dataIndex: 'tier', render: (v) => tiers[v] ?? '—' },
            {
              title: 'اعتبار سریال‌سال', key: 'cs',
              render: (_, r) => {
                const n = credits.data?.items[String(r.id)]?.serial_years ?? 0
                return <Tag color={n === 0 ? 'red' : n < 10 ? 'orange' : 'green'}>{faNum(n)}</Tag>
              },
            },
            { title: 'اعتبار کاربرسال 2FA', key: 'cu', render: (_, r) => faNum(credits.data?.items[String(r.id)]?.user_years ?? 0) },
            { title: 'رابط', dataIndex: ['contact', 'tech_name'], render: (v) => v || '—' },
            { title: 'وضعیت', dataIndex: 'active', render: (v) => (v ? <Tag color="green">فعال</Tag> : <Tag>غیرفعال</Tag>) },
            { title: 'از', dataIndex: 'created_at', render: faDate },
            {
              title: '', key: 'edit',
              render: (_: unknown, r: Org) => (
                <Space>
                  <Button type="link" onClick={() => setCreditOrg(r)}>اعتبار و کاربران</Button>
                  {writable && <Button type="link" onClick={() => setEdit(r)}>ویرایش</Button>}
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <CreditDrawer org={creditOrg} onClose={() => setCreditOrg(null)} onChanged={() => void credits.reload()} />
      <OrgForm kind="reseller" open={edit !== undefined} initial={edit} busy={busy} onCancel={() => setEdit(undefined)} onSubmit={save} />
    </>
  )
}
