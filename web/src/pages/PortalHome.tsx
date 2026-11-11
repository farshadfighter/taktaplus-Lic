import { useState } from 'react'
import { Alert, App, Button, Card, Col, Descriptions, Form, Input, Modal, Row, Space, Statistic, Table, Tag, Typography } from 'antd'
import { Link, useNavigate } from 'react-router'
import { api, type CreditTx, type Item, type LicenseDetail, type LicenseSummary, type Org, type PortalOverview } from '../api'
import { useAuth } from '../auth'
import { saveText } from '../download'
import { daysUntil, faDate, faDateTime, faNum } from '../format'
import OrgUsers from '../OrgUsers'
import { Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'

const platformName = (p: string | null) => (p === 'fortigate' ? 'FortiGate' : p === 'fortiweb' ? 'FortiWeb' : '—')

// itemState is the entitlement of an item today, as the product sees it.
function itemState(it: Item, graceDays: number) {
  const left = daysUntil(it.expires_on) ?? 0
  if (!it.active) return { tag: <Tag>غیرفعال</Tag>, grace: false }
  if (left >= 0) return { tag: <Tag color="green">فعال</Tag>, grace: false }
  if (left + graceDays >= 0) {
    const end = new Date(it.expires_on + 'T12:00:00Z')
    end.setUTCDate(end.getUTCDate() + graceDays)
    return { tag: <Tag color="orange">مهلت تا {faDate(end.toISOString().slice(0, 10))}</Tag>, grace: true }
  }
  return { tag: <Tag color="red">منقضی</Tag>, grace: false }
}

function LicenseCard({ d }: { d: LicenseDetail }) {
  const { message } = App.useApp()
  const usage = d.installation?.last_usage ?? {}
  const devices = d.items.filter((i) => i.kind === 'device' && i.active)
  const download = async () => {
    try {
      const r = await api.post<{ license: string }>(`/portal/licenses/${d.id}/file`)
      saveText(`${d.code}.tplic`, r.license + '\n')
    } catch (e) {
      message.error((e as Error).message)
    }
  }
  return (
    <Card
      title={<Space><span>لایسنس</span><Mono>{d.code}</Mono>{d.status === 'active' ? <Tag color="green">فعال</Tag> : <Tag color="red">باطل</Tag>}</Space>}
      extra={d.installation && d.status === 'active' && <Button size="small" onClick={download}>دانلود license.tplic</Button>}
      style={{ marginBottom: 16 }}
      styles={{ body: { padding: 0 } }}
    >
      <Row gutter={16} style={{ padding: 16 }}>
        <Col xs={12} md={6}><Statistic title="سریال‌ها" value={faNum(devices.length)} suffix={usage.devices !== undefined && <Typography.Text type="secondary" style={{ fontSize: 13 }}>{faNum(usage.devices)} در حال استفاده</Typography.Text>} /></Col>
        <Col xs={12} md={6}><Statistic title="کاربران 2FA" value={faNum(d.otp_users)} suffix={usage.otp_users !== undefined && <Typography.Text type="secondary" style={{ fontSize: 13 }}>{faNum(usage.otp_users)} تعریف‌شده</Typography.Text>} /></Col>
        <Col xs={24} md={12}>
          <Typography.Text type="secondary">سرور TaktaPlus</Typography.Text>
          <div style={{ marginTop: 6 }}>
            {d.installation ? (
              <>
                <Mono>{d.installation.id}</Mono>
                <div style={{ fontSize: 12, color: '#666' }}>
                  {d.installation.last_heartbeat_at ? `متصل · آخرین ارتباط ${faDateTime(d.installation.last_heartbeat_at)}` : d.mode === 'offline' ? 'فعال‌سازی آفلاین' : 'هنوز ارتباطی ثبت نشده'}
                </div>
              </>
            ) : (
              <Typography.Text type="secondary">هنوز فعال نشده است</Typography.Text>
            )}
          </div>
        </Col>
      </Row>
      <Table<Item>
        size="small" rowKey="id" pagination={false} dataSource={d.items.filter((i) => i.active)}
        columns={[
          { title: 'آیتم', dataIndex: 'kind', render: (k) => (k === 'device' ? 'دستگاه' : '2FA') },
          { title: 'سریال / ظرفیت', render: (_, i) => (i.kind === 'device' ? <Mono>{i.serial}</Mono> : `${faNum(i.users)} کاربر`) },
          { title: 'پلتفرم', dataIndex: 'platform', render: platformName },
          { title: 'انقضا', dataIndex: 'expires_on', render: faDate },
          { title: 'وضعیت', render: (_, i) => itemState(i, d.grace_days).tag },
        ]}
      />
    </Card>
  )
}

function CustomerHome({ data }: { data: PortalOverview }) {
  const details = data.details ?? []
  const grace = details.flatMap((d) => d.items.filter((i) => itemState(i, d.grace_days).grace))
  return (
    <>
      <PageHeader title="لایسنس‌های من" subtitle={data.org.name}
        extra={<Link to="/portal/activation"><Button>فعال‌سازی آفلاین</Button></Link>} />
      {grace.length > 0 && (
        <Alert type="warning" showIcon style={{ marginBottom: 16 }}
          title={`${faNum(grace.length)} آیتم در دوره‌ی مهلت است. پس از پایان مهلت، عملیات روی این دستگاه‌ها متوقف می‌شود؛ برای تمدید با فروشنده تماس بگیرید.`} />
      )}
      {details.length === 0 && <Alert type="info" showIcon title="هنوز لایسنسی برای شما صادر نشده است." />}
      {details.map((d) => <LicenseCard key={d.id} d={d} />)}
    </>
  )
}

const delta = (n: number) => (n === 0 ? '—' : <span dir="ltr" style={{ color: n > 0 ? '#2E7D32' : '#B42318' }}>{n > 0 ? '+' : '−'}{faNum(Math.abs(n))}</span>)

function ResellerHome({ data, reload }: { data: PortalOverview; reload: () => void }) {
  const { message } = App.useApp()
  const nav = useNavigate()
  const [custOpen, setCustOpen] = useState(false)
  const [usersOf, setUsersOf] = useState<Org | null>(null)
  const [form] = Form.useForm()
  const credits = data.credits ?? { serial_years: 0, user_years: 0 }
  const lics = data.licenses
  const serials = lics.filter((l) => l.status === 'active').reduce((a, l) => a + l.serials, 0)
  const soon = lics.filter((l) => l.status === 'active' && l.nearest_expiry && (daysUntil(l.nearest_expiry) ?? 999) <= 60)
  const customers = data.customers ?? []
  const withoutLicense = customers.filter((c) => !lics.some((l) => l.customer_id === c.id))
  const tierName: Record<string, string> = { gold: 'نماینده‌ی طلایی', silver: 'نماینده‌ی نقره‌ای', bronze: 'نماینده‌ی برنزی' }
  return (
    <>
      <PageHeader
        title={data.org.name}
        subtitle={`${tierName[data.org.tier] ?? 'نماینده'} · ${faNum(customers.length)} مشتری`}
        extra={<>
          <Button onClick={() => { form.resetFields(); setCustOpen(true) }}>+ مشتری</Button>
          <Button type="primary" onClick={() => nav('/portal/issue')}>صدور / تمدید لایسنس</Button>
        </>}
      />
      <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
        <Col xs={12} md={6}><Card size="small"><Statistic title="اعتبار سریال‌سال" value={faNum(credits.serial_years)} valueStyle={{ color: credits.serial_years < 10 ? '#D46B08' : undefined }} />
          {credits.serial_years < 10 && <Typography.Text type="warning" style={{ fontSize: 12 }}>کمتر از ۱۰ — برای خرید اعتبار با فروش تماس بگیرید</Typography.Text>}</Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="اعتبار کاربرسال 2FA" value={faNum(credits.user_years)} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="سریال‌های فعال مشتریان" value={faNum(serials)} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="انقضا تا ۶۰ روز" value={`${faNum(soon.length)} لایسنس`} /></Card></Col>
      </Row>
      <Card title="مشتریان من" size="small" styles={{ body: { padding: 0 } }} style={{ marginBottom: 16 }}>
        <Table<LicenseSummary>
          size="small" rowKey="id" pagination={false} dataSource={lics} scroll={{ x: true }}
          locale={{ emptyText: 'هنوز لایسنسی صادر نکرده‌اید' }}
          columns={[
            { title: 'مشتری', dataIndex: 'customer_name' },
            { title: 'لایسنس', dataIndex: 'code', render: (v, l) => <>{<Mono>{v}</Mono>} {l.status === 'revoked' && <Tag color="red">باطل</Tag>}</> },
            { title: 'سریال‌ها', dataIndex: 'serials', render: (v) => faNum(v) },
            { title: '2FA', dataIndex: 'otp_users', render: (v) => (v ? faNum(v) : '—') },
            {
              title: 'نزدیک‌ترین انقضا', dataIndex: 'nearest_expiry',
              render: (v) => <span style={{ color: (daysUntil(v) ?? 999) <= 60 ? '#D46B08' : undefined }}>{faDate(v)}</span>,
            },
            { title: 'نصب', render: (_, l) => (l.installation_id ? (l.mode === 'offline' ? 'آفلاین' : 'متصل') : <Typography.Text type="secondary">فعال نشده</Typography.Text>) },
            {
              title: '', key: 'a',
              render: (_, l) => (
                <Space>
                  {l.status === 'active' && <Button size="small" type="link" onClick={() => nav(`/portal/issue?license=${l.id}`)}>تمدید / افزودن</Button>}
                  <Button size="small" type="link" onClick={() => setUsersOf(customers.find((c) => c.id === l.customer_id) ?? null)}>کاربران</Button>
                </Space>
              ),
            },
          ]}
        />
        {withoutLicense.length > 0 && (
          <div style={{ padding: 12, borderTop: '1px solid #F0F0F0' }}>
            <Typography.Text type="secondary">مشتریان بدون لایسنس: </Typography.Text>
            {withoutLicense.map((c) => (
              <Tag key={c.id} style={{ cursor: 'pointer' }} onClick={() => nav(`/portal/issue?customer=${c.id}`)}>{c.name}</Tag>
            ))}
          </div>
        )}
      </Card>
      <Card title="تراکنش‌های اعتبار" size="small" styles={{ body: { padding: 0 } }}>
        <Table<CreditTx> size="small" rowKey="id" pagination={false} dataSource={data.transactions ?? []} locale={{ emptyText: 'تراکنشی نیست' }}
          columns={[
            { title: 'زمان', dataIndex: 'at', render: (v) => faDateTime(v) },
            { title: 'شرح', dataIndex: 'description' },
            { title: 'سریال‌سال', dataIndex: 'serial_years', render: delta },
            { title: 'کاربرسال', dataIndex: 'user_years', render: delta },
            { title: 'مانده', render: (_, t) => `${faNum(t.balance_serial)} / ${faNum(t.balance_user)}` },
          ]} />
      </Card>

      <Modal open={custOpen} title="مشتری جدید" okText="ایجاد" cancelText="انصراف" onCancel={() => setCustOpen(false)} onOk={() => form.submit()} destroyOnHidden>
        <Form form={form} layout="vertical" requiredMark={false} onFinish={async (v) => {
          try {
            await api.post('/portal/customers', { name: v.name, legal_name: v.legal_name ?? '', national_id: v.national_id ?? '', contact: { tech_name: v.tech_name ?? '', tech_phone: '', tech_email: v.tech_email ?? '', finance_name: '' } })
            message.success('مشتری ایجاد شد')
            setCustOpen(false)
            reload()
          } catch (e) {
            message.error((e as Error).message)
          }
        }}>
          <Form.Item name="name" label="نام" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="legal_name" label="نام حقوقی"><Input /></Form.Item>
          <Form.Item name="national_id" label="شناسه‌ی ملی"><Input dir="ltr" /></Form.Item>
          <Form.Item name="tech_name" label="رابط فنی"><Input /></Form.Item>
          <Form.Item name="tech_email" label="ایمیل رابط فنی"><Input dir="ltr" /></Form.Item>
        </Form>
      </Modal>
      <Modal open={usersOf !== null} title={`کاربران پورتال ${usersOf?.name ?? ''}`} footer={null} onCancel={() => setUsersOf(null)} width={720} destroyOnHidden>
        {usersOf && (
          <>
            <Descriptions size="small" column={1} items={[{ key: 'c', label: 'کد مشتری', children: <Mono>{usersOf.code}</Mono> }]} />
            <OrgUsers base={`/portal/customers/${usersOf.id}/users`} writable />
          </>
        )}
      </Modal>
    </>
  )
}

export default function PortalHome() {
  const { user } = useAuth()
  const { data, error, reload } = useFetch<PortalOverview>('/portal/overview')
  if (error) return <Alert type="error" showIcon title={error} />
  if (!data) return null
  return user?.role === 'reseller' ? <ResellerHome data={data} reload={() => void reload()} /> : <CustomerHome data={data} />
}
