import { useState } from 'react'
import { App, Button, Card, Checkbox, Col, Descriptions, Form, Input, Modal, Row, Select, Space, Table, Tabs, Tag, Typography } from 'antd'
import { Link, useParams } from 'react-router'
import { api, type Item, type LicenseDetail as Detail } from '../api'
import { can, useAuth } from '../auth'
import { addYears, faDate, faDateTime, faNum, isoToday, platformLabel } from '../format'
import { saveText } from '../download'
import { ExpiryTag, Mono, PageHeader, statusTag, warningLabels } from '../ui'
import { useFetch } from '../useFetch'

type Dialog = 'renew' | 'add' | 'rma' | 'revoke' | null

const yearOptions = [1, 2, 3].map((y) => ({ value: y, label: `${faNum(y)} سال` }))

// laterOf returns the later of two ISO dates.
const laterOf = (a: string, b: string) => (a > b ? a : b)

export default function LicenseDetail() {
  const { id } = useParams()
  const lic = useFetch<Detail>(`/licenses/${id}`)
  const [dialog, setDialog] = useState<Dialog>(null)
  const [busy, setBusy] = useState(false)
  const [key, setKey] = useState<string | null>(null)
  const { user } = useAuth()
  const { message, modal } = App.useApp()
  const d = lic.data
  const active = d?.status === 'active'
  const writable = can(user, 'licenses.write') && active

  async function act(fn: () => Promise<Detail | void>, ok?: string) {
    setBusy(true)
    try {
      const res = await fn()
      if (res) lic.setData(res)
      setDialog(null)
      if (ok) message.success(ok)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const download = () =>
    act(async () => {
      saveText(`${d?.code}.tplic`, await api.post<string>(`/licenses/${id}/file`))
      void lic.reload()
    })

  const release = () =>
    modal.confirm({
      title: 'آزادسازی نصب',
      content: 'نصب فعلی دیگر لایسنس دریافت نمی‌کند و کد فعال‌سازی روی سرور جدید قابل استفاده می‌شود.',
      okText: 'آزادسازی', cancelText: 'انصراف',
      onOk: () => act(() => api.post<Detail>(`/licenses/${id}/release`), 'نصب آزاد شد'),
    })

  const items = d?.items.filter((i) => i.active) ?? []
  const devices = items.filter((i) => i.kind === 'device')

  return (
    <>
      <PageHeader
        title={<Space><Mono>{d?.code ?? '…'}</Mono>{d && statusTag(d.status)}</Space>}
        subtitle={d && <Link to={`/customers/${d.customer_id}`}>{d.customer_name}</Link>}
        extra={<>
          {writable && <Button type="primary" onClick={() => setDialog('renew')}>تمدید</Button>}
          {writable && <Button onClick={() => setDialog('add')}>افزودن آیتم</Button>}
          {writable && devices.length > 0 && <Button onClick={() => setDialog('rma')}>جایگزینی سریال (RMA)</Button>}
          {active && d?.installation_id && can(user, 'licenses.write') && <Button onClick={download} loading={busy}>دانلود .tplic</Button>}
          {active && can(user, 'licenses.revoke') && <Button danger onClick={() => setDialog('revoke')}>باطل کردن</Button>}
        </>}
      />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card loading={lic.loading}>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="کد فعال‌سازی">
                {key ? <Typography.Text copyable code dir="ltr">{key}</Typography.Text>
                  : <Space><Mono>TPK-••••-••••-••••-••••</Mono>{can(user, 'licenses.write') && <Button size="small" type="link" onClick={() => act(async () => setKey((await api.post<{ activation_key: string }>(`/licenses/${id}/reveal-key`)).activation_key))}>نمایش</Button>}</Space>}
              </Descriptions.Item>
              <Descriptions.Item label="روش">{d?.mode === 'online' ? 'آنلاین و آفلاین' : 'فقط آفلاین'} · Heartbeat هر {faNum(d?.heartbeat_hours)} ساعت</Descriptions.Item>
              <Descriptions.Item label="دوره‌ی مهلت">{faNum(d?.grace_days)} روز</Descriptions.Item>
              <Descriptions.Item label="نماینده">{d?.reseller_name || 'فروش مستقیم'}</Descriptions.Item>
              <Descriptions.Item label="سفارش">{d?.order_ref || '—'}</Descriptions.Item>
              <Descriptions.Item label="صدور">{faDate(d?.created_at)}</Descriptions.Item>
              {d?.revoked_at && <Descriptions.Item label="ابطال">{faDate(d.revoked_at)}</Descriptions.Item>}
            </Descriptions>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card loading={lic.loading} title="نصب متصل">
            {d?.installation ? (
              <>
                <Descriptions column={1} size="small">
                  <Descriptions.Item label="شناسه"><Mono>{d.installation.id}</Mono></Descriptions.Item>
                  <Descriptions.Item label="نسخه"><Mono>{d.installation.product_version || '—'}</Mono></Descriptions.Item>
                  <Descriptions.Item label="آخرین ارتباط">{faDateTime(d.installation.last_heartbeat_at)} <Mono>{d.installation.last_seen_ip}</Mono></Descriptions.Item>
                  <Descriptions.Item label="مصرف گزارش‌شده">{faNum(d.installation.last_usage.devices ?? 0)} دستگاه · {faNum(d.installation.last_usage.otp_users ?? 0)} کاربر 2FA</Descriptions.Item>
                  <Descriptions.Item label="هشدارها">{d.installation.warnings.length ? d.installation.warnings.map((w) => <Tag key={w} color="red">{warningLabels[w] ?? w}</Tag>) : <Tag color="green">سالم</Tag>}</Descriptions.Item>
                </Descriptions>
                {active && can(user, 'installations.write') && <Button style={{ marginTop: 12 }} onClick={release}>آزادسازی نصب (انتقال به سرور جدید)</Button>}
              </>
            ) : <Typography.Text type="secondary">هنوز فعال نشده است. کد فعال‌سازی را در پنل TaktaPlus وارد کنید، یا فایل درخواست را در «فعال‌سازی آفلاین» بارگذاری کنید.</Typography.Text>}
          </Card>
        </Col>
      </Row>

      <Card style={{ marginTop: 16 }}>
        <Tabs items={[
          {
            key: 'items', label: `آیتم‌ها (${faNum(items.length)})`,
            children: <Table rowKey="id" size="small" pagination={false} scroll={{ x: true }} dataSource={d?.items ?? []}
              columns={[
                { title: 'نوع', dataIndex: 'kind', render: (v) => (v === 'device' ? 'دستگاه' : '2FA') },
                { title: 'سریال / ظرفیت', key: 'what', render: (_, r: Item) => (r.kind === 'device' ? <Mono>{r.serial}</Mono> : `${faNum(r.users)} کاربر`) },
                { title: 'پلتفرم', dataIndex: 'platform', render: platformLabel },
                { title: 'شروع', dataIndex: 'starts_on', render: faDate },
                { title: 'انقضا', dataIndex: 'expires_on', render: (v, r) => (r.active ? <ExpiryTag date={v} grace={d?.grace_days} /> : <Tag>غیرفعال</Tag>) },
              ]} />,
          },
          {
            key: 'files', label: 'فایل‌های صادرشده',
            children: <Table rowKey="id" size="small" pagination={false} scroll={{ x: true }} dataSource={d?.files ?? []}
              columns={[
                { title: 'زمان', dataIndex: 'at', render: faDateTime },
                { title: 'علت', dataIndex: 'reason' },
                { title: 'تحویل', dataIndex: 'delivery' },
                { title: 'کلید', dataIndex: 'kid', render: (v) => <Mono>{v}</Mono> },
                { title: 'SHA-256', dataIndex: 'sha256', render: (v: string) => <Mono>{v.slice(0, 12)}…</Mono> },
              ]} />,
          },
          {
            key: 'rma', label: 'تاریخچه‌ی سریال',
            children: <Table rowKey="at" size="small" pagination={false} dataSource={d?.serial_changes ?? []}
              columns={[
                { title: 'زمان', dataIndex: 'at', render: faDateTime },
                { title: 'قبلی', dataIndex: 'old_serial', render: (v) => <Mono>{v}</Mono> },
                { title: 'جدید', dataIndex: 'new_serial', render: (v) => <Mono>{v}</Mono> },
                { title: 'علت', dataIndex: 'reason' },
              ]} />,
          },
        ]} />
      </Card>

      <RenewDialog open={dialog === 'renew'} items={items} busy={busy} onCancel={() => setDialog(null)}
        onOk={(ids, exp) => act(() => api.post<Detail>(`/licenses/${id}/renew`, { item_ids: ids, expires_on: exp }), 'تمدید شد')} />
      <AddDialog open={dialog === 'add'} busy={busy} onCancel={() => setDialog(null)}
        onOk={(it) => act(() => api.post<Detail>(`/licenses/${id}/items`, { items: it }), 'آیتم‌ها افزوده شدند')} />
      <RMADialog open={dialog === 'rma'} devices={devices} busy={busy} onCancel={() => setDialog(null)}
        onOk={(v) => act(() => api.post<Detail>(`/licenses/${id}/rma`, v), 'سریال جایگزین شد')} />
      <RevokeDialog open={dialog === 'revoke'} busy={busy} onCancel={() => setDialog(null)}
        onOk={(reason) => act(() => api.post<Detail>(`/licenses/${id}/revoke`, { reason }), 'لایسنس باطل شد')} />
    </>
  )
}

function RenewDialog({ open, items, busy, onCancel, onOk }: {
  open: boolean; items: Item[]; busy: boolean; onCancel: () => void; onOk: (ids: number[], expires: string) => void
}) {
  const [ids, setIds] = useState<number[]>([])
  const [years, setYears] = useState(1)
  const chosen = items.filter((i) => ids.includes(i.id))
  // Extend from the latest chosen expiry, or from today if already expired.
  const base = chosen.reduce((m, i) => laterOf(m, i.expires_on), isoToday())
  const next = addYears(base, years)
  const label = (i: Item) => (i.kind === 'device' ? `${i.serial}` : `${i.users} کاربر 2FA`) + ` — ${faDate(i.expires_on)}`
  return (
    <Modal open={open} title="تمدید" okText="تمدید" cancelText="انصراف" confirmLoading={busy} onCancel={onCancel}
      okButtonProps={{ disabled: ids.length === 0 }} onOk={() => onOk(ids, next)} destroyOnHidden>
      <Checkbox.Group style={{ display: 'flex', flexDirection: 'column', gap: 6 }} value={ids} onChange={(v) => setIds(v as number[])}
        options={items.map((i) => ({ value: i.id, label: label(i) }))} />
      <Space style={{ marginTop: 16 }}>
        مدت: <Select value={years} onChange={setYears} options={yearOptions} style={{ width: 100 }} />
        انقضای جدید: <b>{faDate(next)}</b>
      </Space>
    </Modal>
  )
}

function AddDialog({ open, busy, onCancel, onOk }: {
  open: boolean; busy: boolean; onCancel: () => void; onOk: (items: unknown[]) => void
}) {
  const [serials, setSerials] = useState('')
  const [users, setUsers] = useState(0)
  const [years, setYears] = useState(1)
  const start = isoToday()
  const submit = () => {
    const exp = addYears(start, years)
    const list = serials.split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean)
    onOk([
      ...list.map((serial) => ({ kind: 'device', serial, starts_on: start, expires_on: exp })),
      ...(users > 0 ? [{ kind: 'otp', users, starts_on: start, expires_on: exp }] : []),
    ])
  }
  return (
    <Modal open={open} title="افزودن آیتم" okText="افزودن" cancelText="انصراف" confirmLoading={busy} onCancel={onCancel} onOk={submit} destroyOnHidden>
      <Form layout="vertical">
        <Form.Item label="سریال‌های جدید (هر خط یک سریال)"><Input.TextArea rows={4} dir="ltr" value={serials} onChange={(e) => setSerials(e.target.value)} /></Form.Item>
        <Form.Item label="کاربران 2FA (اختیاری)"><Input type="number" min={0} value={users || ''} onChange={(e) => setUsers(Number(e.target.value))} /></Form.Item>
        <Form.Item label="مدت"><Select value={years} onChange={setYears} options={yearOptions} /></Form.Item>
        <Typography.Text type="secondary">از امروز تا {faDate(addYears(start, years))}</Typography.Text>
      </Form>
    </Modal>
  )
}

function RMADialog({ open, devices, busy, onCancel, onOk }: {
  open: boolean; devices: Item[]; busy: boolean; onCancel: () => void; onOk: (v: { item_id: number; new_serial: string; reason: string }) => void
}) {
  const [form] = Form.useForm()
  return (
    <Modal open={open} title="جایگزینی سریال (RMA)" okText="جایگزینی" cancelText="انصراف" confirmLoading={busy} onCancel={onCancel} onOk={() => form.submit()} destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={onOk} requiredMark={false}>
        <Form.Item name="item_id" label="سریال فعلی" rules={[{ required: true }]}>
          <Select options={devices.map((i) => ({ value: i.id, label: i.serial }))} />
        </Form.Item>
        <Form.Item name="new_serial" label="سریال جدید" rules={[{ required: true }]}><Input dir="ltr" /></Form.Item>
        <Form.Item name="reason" label="علت" initialValue=""><Input /></Form.Item>
        <Typography.Text type="secondary">تاریخ انقضا حفظ می‌شود.</Typography.Text>
      </Form>
    </Modal>
  )
}

function RevokeDialog({ open, busy, onCancel, onOk }: { open: boolean; busy: boolean; onCancel: () => void; onOk: (reason: string) => void }) {
  const [reason, setReason] = useState('')
  return (
    <Modal open={open} title="باطل کردن لایسنس" okText="باطل کن" okButtonProps={{ danger: true, disabled: !reason.trim() }} cancelText="انصراف"
      confirmLoading={busy} onCancel={onCancel} onOk={() => onOk(reason)} destroyOnHidden>
      <p>این کار قابل بازگشت نیست. سریال‌ها آزاد می‌شوند و نصب متصل در Heartbeat بعدی لایسنس را از دست می‌دهد.</p>
      <Input.TextArea rows={2} placeholder="علت" value={reason} onChange={(e) => setReason(e.target.value)} />
    </Modal>
  )
}
