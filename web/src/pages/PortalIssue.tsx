import { useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, Card, Checkbox, Col, Descriptions, Input, Modal, Row, Segmented, Select, Space, Table, Typography } from 'antd'
import { useNavigate, useSearchParams } from 'react-router'
import { api, type Item, type LicenseDetail, type PortalOverview } from '../api'
import { faDate, faNum } from '../format'
import { Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'

type Mode = 'new' | 'add' | 'renew'

const plusYears = (iso: string, years: number) => {
  const today = new Date().toISOString().slice(0, 10)
  const base = iso > today ? iso : today
  const d = new Date(base + 'T12:00:00Z')
  d.setUTCFullYear(d.getUTCFullYear() + years)
  return d.toISOString().slice(0, 10)
}

// PortalIssue lets a reseller issue, extend or renew licenses from its
// prepaid credit.
export default function PortalIssue() {
  const { message } = App.useApp()
  const nav = useNavigate()
  const [params] = useSearchParams()
  const overview = useFetch<PortalOverview>('/portal/overview')
  const [mode, setMode] = useState<Mode>(params.get('license') ? 'renew' : 'new')
  const [customer, setCustomer] = useState<number | null>(params.get('customer') ? Number(params.get('customer')) : null)
  const [licenseID, setLicenseID] = useState<number | null>(params.get('license') ? Number(params.get('license')) : null)
  const [detail, setDetail] = useState<LicenseDetail | null>(null)
  const [serials, setSerials] = useState('')
  const [otp, setOtp] = useState(0)
  const [years, setYears] = useState(1)
  const [renewIDs, setRenewIDs] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const [issuedKey, setIssuedKey] = useState<{ code: string; key: string } | null>(null)

  useEffect(() => {
    setDetail(null)
    setRenewIDs([])
    if (licenseID && mode !== 'new') {
      api.get<LicenseDetail>(`/portal/licenses/${licenseID}`).then((d) => {
        setDetail(d)
        setRenewIDs(d.items.filter((i) => i.active && (new Date(i.expires_on).getTime() - Date.now()) / 86_400_000 <= 90).map((i) => i.id))
      }).catch((e) => message.error(e.message))
    }
  }, [licenseID, mode, message])

  const credits = overview.data?.credits ?? { serial_years: 0, user_years: 0 }
  const serialList = serials.split(/[\s,]+/).map((s) => s.trim().toUpperCase()).filter(Boolean)
  const cost = useMemo(() => {
    if (mode === 'renew') {
      const items = detail?.items.filter((i) => renewIDs.includes(i.id)) ?? []
      return {
        serial: items.filter((i) => i.kind === 'device').length * years,
        user: items.reduce((a, i) => a + (i.users ?? 0), 0) * years,
      }
    }
    return { serial: serialList.length * years, user: otp * years }
  }, [mode, detail, renewIDs, years, serialList.length, otp])
  const leftSerial = credits.serial_years - cost.serial
  const leftUser = credits.user_years - cost.user
  const short = leftSerial < 0 || leftUser < 0
  const empty = cost.serial === 0 && cost.user === 0

  const submit = async () => {
    setBusy(true)
    try {
      if (mode === 'new') {
        const r = await api.post<{ license: LicenseDetail; activation_key: string }>('/portal/licenses', { customer_id: customer, serials: serialList, otp_users: otp, years })
        setIssuedKey({ code: r.license.code, key: r.activation_key })
      } else if (mode === 'add') {
        await api.post(`/portal/licenses/${licenseID}/items`, { serials: serialList, otp_users: otp, years })
        message.success('به لایسنس اضافه شد')
        nav('/portal')
      } else {
        await api.post(`/portal/licenses/${licenseID}/renew`, { item_ids: renewIDs, years })
        message.success('تمدید شد')
        nav('/portal')
      }
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const lics = (overview.data?.licenses ?? []).filter((l) => l.status === 'active')
  const customers = overview.data?.customers ?? []
  const ready = mode === 'new' ? customer !== null : licenseID !== null

  return (
    <>
      <PageHeader title="صدور و تمدید لایسنس" subtitle="هزینه از اعتبار پیش‌خرید شما کسر می‌شود" />
      <Row gutter={16}>
        <Col xs={24} lg={16}>
          <Card>
            <Segmented value={mode} onChange={(v) => setMode(v as Mode)} style={{ marginBottom: 16 }}
              options={[{ value: 'new', label: 'لایسنس جدید' }, { value: 'add', label: 'افزودن به لایسنس' }, { value: 'renew', label: 'تمدید' }]} />
            <Space orientation="vertical" style={{ width: '100%' }} size="middle">
              {mode === 'new' ? (
                <Select placeholder="مشتری" value={customer} onChange={setCustomer} style={{ width: '100%' }} showSearch optionFilterProp="label"
                  options={customers.map((c) => ({ value: c.id, label: c.name }))} />
              ) : (
                <Select placeholder="لایسنس" value={licenseID} onChange={setLicenseID} style={{ width: '100%' }} showSearch optionFilterProp="label"
                  options={lics.map((l) => ({ value: l.id, label: `${l.customer_name} (${l.code})` }))} />
              )}
              {mode === 'renew' ? (
                detail && (
                  <Table<Item>
                    size="small" rowKey="id" pagination={false} dataSource={detail.items.filter((i) => i.active)}
                    rowSelection={{ selectedRowKeys: renewIDs, onChange: (k) => setRenewIDs(k as number[]) }}
                    columns={[
                      { title: 'سریال / ظرفیت', render: (_, i) => (i.kind === 'device' ? <Mono>{i.serial}</Mono> : `2FA · ${faNum(i.users)} کاربر`) },
                      { title: 'انقضای فعلی', dataIndex: 'expires_on', render: faDate },
                      { title: 'انقضای جدید', render: (_, i) => (renewIDs.includes(i.id) ? faDate(plusYears(i.expires_on, years)) : '—') },
                    ]}
                  />
                )
              ) : (
                <>
                  <div>
                    <Typography.Text>سریال‌های جدید (هر خط یک سریال)</Typography.Text>
                    <Input.TextArea rows={4} dir="ltr" value={serials} onChange={(e) => setSerials(e.target.value)} placeholder={'FG100FTK2201310\nFG100FTK2201311'} />
                  </div>
                  <div>
                    <Typography.Text>بسته‌ی کاربر 2FA (اختیاری) </Typography.Text>
                    <Select value={otp} onChange={setOtp} style={{ width: 160 }}
                      options={[0, 25, 50, 100, 250, 500, 1000].map((n) => ({ value: n, label: n ? `${faNum(n)} کاربر` : '—' }))} />
                  </div>
                </>
              )}
              <div>
                <Typography.Text>مدت </Typography.Text>
                <Segmented value={years} onChange={(v) => setYears(Number(v))} options={[1, 2, 3].map((y) => ({ value: y, label: `${faNum(y)} سال` }))} />
              </div>
            </Space>
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="خلاصه">
            <Descriptions column={1} size="small" items={[
              { key: 's', label: 'کسر از اعتبار سریال‌سال', children: faNum(cost.serial) },
              { key: 'sl', label: 'مانده‌ی سریال‌سال پس از ثبت', children: <span style={{ color: leftSerial < 0 ? '#B42318' : undefined }}>{faNum(leftSerial)}</span> },
              { key: 'u', label: 'کسر از اعتبار کاربرسال', children: faNum(cost.user) },
              { key: 'ul', label: 'مانده‌ی کاربرسال پس از ثبت', children: <span style={{ color: leftUser < 0 ? '#B42318' : undefined }}>{faNum(leftUser)}</span> },
            ]} />
            {short && <Alert type="error" showIcon title="اعتبار کافی نیست؛ برای خرید اعتبار با فروش تماس بگیرید." style={{ marginTop: 12 }} />}
            {!short && leftSerial < 10 && !empty && <Alert type="warning" showIcon title="اعتبار شما رو به اتمام است." style={{ marginTop: 12 }} />}
            <Button type="primary" block style={{ marginTop: 16 }} disabled={!ready || empty || short} loading={busy} onClick={submit}>ثبت</Button>
            <Typography.Paragraph type="secondary" style={{ marginTop: 12, fontSize: 12 }}>
              نصب‌های آنلاین مشتری لایسنس جدید را در Heartbeat بعدی (حداکثر ۲۴ ساعت) دریافت می‌کنند؛ برای نصب‌های آفلاین از «فعال‌سازی آفلاین» فایل لایسنس بسازید.
            </Typography.Paragraph>
          </Card>
        </Col>
      </Row>
      <Modal open={issuedKey !== null} title="لایسنس صادر شد" okText="بازگشت به داشبورد" cancelButtonProps={{ style: { display: 'none' } }}
        onOk={() => nav('/portal')} onCancel={() => nav('/portal')}>
        <Typography.Paragraph>لایسنس <Mono>{issuedKey?.code}</Mono> صادر شد. این کد فعال‌سازی را به مشتری بدهید:</Typography.Paragraph>
        <Typography.Paragraph copyable={{ text: issuedKey?.key }}><Mono>{issuedKey?.key}</Mono></Typography.Paragraph>
        <Checkbox checked disabled>از اعتبار شما کسر شد</Checkbox>
      </Modal>
    </>
  )
}
