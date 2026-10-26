import { useEffect, useState } from 'react'
import { Alert, App, Button, Card, Col, Form, Input, InputNumber, Modal, Row, Select, Space, Typography } from 'antd'
import { useNavigate, useSearchParams } from 'react-router'
import { api, type ItemInput, type LicenseDetail, type Org } from '../api'
import { addYears, faDate, faNum, isoToday } from '../format'
import { Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'

type Values = {
  customer_id: number
  reseller_id: number | null
  mode: 'online' | 'offline'
  grace_days: number
  heartbeat_hours: number
  order_ref: string
  notes: string
  starts_on: string
  devices: { serial: string; years: number }[]
  otp: { users: number; years: number }[]
}

const yearOptions = [1, 2, 3].map((y) => ({ value: y, label: `${faNum(y)} سال` }))

export default function LicenseNew() {
  const [form] = Form.useForm<Values>()
  const [params] = useSearchParams()
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<{ license: LicenseDetail; activation_key: string } | null>(null)
  const [paste, setPaste] = useState('')
  const { message } = App.useApp()
  const navigate = useNavigate()
  const customers = useFetch<{ items: Org[] }>('/customers')
  const resellers = useFetch<{ items: Org[] }>('/resellers')
  const start = Form.useWatch('starts_on', form) ?? isoToday()
  const devices = Form.useWatch('devices', form) ?? []
  const otp = Form.useWatch('otp', form) ?? []

  useEffect(() => {
    const cid = Number(params.get('customer_id'))
    if (cid) form.setFieldValue('customer_id', cid)
  }, [params, form])

  const onCustomer = (id: number) => {
    const c = customers.data?.items.find((x) => x.id === id)
    form.setFieldValue('reseller_id', c?.parent_id ?? null)
  }

  const addPasted = () => {
    const serials = paste.split(/[\s,;]+/).map((s) => s.trim().toUpperCase()).filter(Boolean)
    const cur = (form.getFieldValue('devices') as Values['devices']) ?? []
    const have = new Set(cur.map((d) => d.serial?.toUpperCase()))
    form.setFieldValue('devices', [...cur.filter((d) => d.serial), ...serials.filter((s) => !have.has(s)).map((serial) => ({ serial, years: 1 }))])
    setPaste('')
  }

  const submit = async (v: Values) => {
    const items: ItemInput[] = [
      ...(v.devices ?? []).filter((d) => d.serial).map((d) => ({ kind: 'device' as const, serial: d.serial.trim(), starts_on: v.starts_on, expires_on: addYears(v.starts_on, d.years) })),
      ...(v.otp ?? []).filter((o) => o.users > 0).map((o) => ({ kind: 'otp' as const, users: o.users, starts_on: v.starts_on, expires_on: addYears(v.starts_on, o.years) })),
    ]
    if (items.length === 0) {
      message.error('حداقل یک سریال یا بسته‌ی 2FA لازم است')
      return
    }
    setBusy(true)
    try {
      setCreated(await api.post('/licenses', {
        customer_id: v.customer_id, reseller_id: v.reseller_id ?? null, mode: v.mode, grace_days: v.grace_days,
        heartbeat_hours: v.heartbeat_hours, order_ref: v.order_ref ?? '', notes: v.notes ?? '', items,
      }))
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader title="صدور لایسنس" subtitle="برای تمدید یا افزودن سریال، از صفحه‌ی همان لایسنس اقدام کنید" />
      <Form form={form} layout="vertical" onFinish={submit} requiredMark={false}
        initialValues={{ mode: 'online', grace_days: 14, heartbeat_hours: 24, starts_on: isoToday(), devices: [{ serial: '', years: 1 }], otp: [] }}>
        <Row gutter={[16, 16]}>
          <Col xs={24} xl={16}>
            <Space orientation="vertical" size={16} style={{ width: '100%' }}>
              <Card title="مشتری و تنظیمات">
                <Row gutter={16}>
                  <Col xs={24} md={12}>
                    <Form.Item name="customer_id" label="مشتری" rules={[{ required: true, message: 'مشتری را انتخاب کنید' }]}>
                      <Select showSearch={{ optionFilterProp: 'label' }} loading={customers.loading} onChange={onCustomer}
                        options={(customers.data?.items ?? []).map((c) => ({ value: c.id, label: `${c.name} (${c.code})` }))} />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item name="reseller_id" label="نماینده">
                      <Select allowClear placeholder="فروش مستقیم" options={(resellers.data?.items ?? []).map((r) => ({ value: r.id, label: r.name }))} />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={8}>
                    <Form.Item name="mode" label="روش فعال‌سازی">
                      <Select options={[{ value: 'online', label: 'آنلاین و آفلاین' }, { value: 'offline', label: 'فقط آفلاین' }]} />
                    </Form.Item>
                  </Col>
                  <Col xs={12} md={8}><Form.Item name="grace_days" label="دوره‌ی مهلت (روز)"><InputNumber min={0} max={365} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col xs={12} md={8}><Form.Item name="heartbeat_hours" label="فاصله‌ی Heartbeat (ساعت)"><InputNumber min={1} max={720} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col xs={24} md={8}><Form.Item name="starts_on" label="تاریخ شروع (میلادی)"><Input type="date" dir="ltr" /></Form.Item></Col>
                  <Col xs={24} md={16}><Form.Item name="order_ref" label="شماره‌ی سفارش / پیش‌فاکتور"><Input /></Form.Item></Col>
                </Row>
              </Card>

              <Card title="اشتراک دستگاه‌ها (سریال)">
                <Form.List name="devices">
                  {(fields, { add, remove }) => (
                    <>
                      {fields.map((f) => (
                        <Space key={f.key} wrap style={{ display: 'flex', marginBottom: 8 }}>
                          <Form.Item name={[f.name, 'serial']} noStyle><Input dir="ltr" placeholder="FG100FTK…" style={{ width: 240, fontFamily: 'monospace' }} /></Form.Item>
                          <Form.Item name={[f.name, 'years']} noStyle><Select options={yearOptions} style={{ width: 100 }} /></Form.Item>
                          <Typography.Text type="secondary">تا {faDate(addYears(start, devices[f.name]?.years ?? 1))}</Typography.Text>
                          <Button type="link" danger onClick={() => remove(f.name)}>حذف</Button>
                        </Space>
                      ))}
                      <Button onClick={() => add({ serial: '', years: 1 })}>+ ردیف</Button>
                    </>
                  )}
                </Form.List>
                <div style={{ marginTop: 16, display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <Typography.Text type="secondary">یا فهرست سریال‌ها را بچسبانید (هر خط یک سریال):</Typography.Text>
                  <Input.TextArea rows={3} dir="ltr" value={paste} onChange={(e) => setPaste(e.target.value)} style={{ fontFamily: 'monospace' }} />
                  <div><Button onClick={addPasted} disabled={!paste.trim()}>افزودن به فهرست</Button></div>
                </div>
              </Card>

              <Card title="بسته‌های کاربر 2FA">
                <Form.List name="otp">
                  {(fields, { add, remove }) => (
                    <>
                      {fields.map((f) => (
                        <Space key={f.key} wrap style={{ display: 'flex', marginBottom: 8 }}>
                          <Form.Item name={[f.name, 'users']} noStyle><InputNumber min={1} placeholder="تعداد کاربر" style={{ width: 160 }} /></Form.Item>
                          <Form.Item name={[f.name, 'years']} noStyle><Select options={yearOptions} style={{ width: 100 }} /></Form.Item>
                          <Typography.Text type="secondary">تا {faDate(addYears(start, otp[f.name]?.years ?? 1))}</Typography.Text>
                          <Button type="link" danger onClick={() => remove(f.name)}>حذف</Button>
                        </Space>
                      ))}
                      <Button onClick={() => add({ users: 50, years: 1 })}>+ بسته</Button>
                    </>
                  )}
                </Form.List>
              </Card>
            </Space>
          </Col>
          <Col xs={24} xl={8}>
            <Card title="خلاصه">
              <p>سریال‌ها: <b>{faNum(devices.filter((d) => d?.serial).length)}</b></p>
              <p>کاربران 2FA: <b>{faNum(otp.reduce((n, o) => n + (o?.users ?? 0), 0))}</b></p>
              <Form.Item name="notes" label="یادداشت"><Input.TextArea rows={3} /></Form.Item>
              <Alert type="info" showIcon title="پس از ثبت، کد فعال‌سازی نمایش داده می‌شود تا برای مشتری بفرستید." style={{ marginBottom: 16 }} />
              <Button type="primary" htmlType="submit" block loading={busy}>ثبت و صدور</Button>
            </Card>
          </Col>
        </Row>
      </Form>
      <Modal
        open={created !== null} closable={false} maskClosable={false} cancelButtonProps={{ style: { display: 'none' } }}
        okText="مشاهده‌ی لایسنس" onOk={() => created && navigate(`/licenses/${created.license.id}`)} title="لایسنس صادر شد"
      >
        <p>لایسنس <Mono>{created?.license.code}</Mono> برای {created?.license.customer_name} ساخته شد.</p>
        <p>کد فعال‌سازی را برای مشتری بفرستید:</p>
        <Typography.Paragraph copyable code style={{ fontSize: 18, direction: 'ltr', textAlign: 'center' }}>{created?.activation_key}</Typography.Paragraph>
      </Modal>
    </>
  )
}
