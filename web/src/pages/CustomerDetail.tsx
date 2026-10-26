import { useState } from 'react'
import { App, Button, Card, Col, Descriptions, Row, Table, Tag } from 'antd'
import { Link, useParams } from 'react-router'
import { api, type LicenseSummary, type Org } from '../api'
import { can, useAuth } from '../auth'
import { faDate, faNum } from '../format'
import { ExpiryTag, Mono, PageHeader, statusTag } from '../ui'
import { useFetch } from '../useFetch'
import OrgForm, { type OrgFormValues } from './OrgForm'

export default function CustomerDetail() {
  const { id } = useParams()
  const org = useFetch<Org>(`/customers/${id}`)
  const lics = useFetch<{ items: LicenseSummary[] }>(`/licenses?customer_id=${id}`)
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const { user } = useAuth()
  const { message } = App.useApp()
  const o = org.data

  const save = async (v: OrgFormValues) => {
    setBusy(true)
    try {
      org.setData(await api.put<Org>(`/customers/${id}`, v))
      setEditing(false)
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title={<>{o?.name ?? '…'} {o && <Tag><Mono>{o.code}</Mono></Tag>}</>}
        subtitle={<Link to="/customers">بازگشت به مشتریان</Link>}
        extra={can(user, 'customers.write') && <>
          <Button onClick={() => setEditing(true)}>ویرایش</Button>
          <Link to={`/licenses/new?customer_id=${id}`}><Button type="primary">+ لایسنس جدید</Button></Link>
        </>}
      />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={14}>
          <Card title="اطلاعات" loading={org.loading}>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="نام حقوقی">{o?.legal_name || '—'}</Descriptions.Item>
              <Descriptions.Item label="شناسه‌ی ملی"><Mono>{o?.national_id || '—'}</Mono></Descriptions.Item>
              <Descriptions.Item label="رابط فنی">{[o?.contact.tech_name, o?.contact.tech_phone, o?.contact.tech_email].filter(Boolean).join(' · ') || '—'}</Descriptions.Item>
              <Descriptions.Item label="رابط مالی">{o?.contact.finance_name || '—'}</Descriptions.Item>
              <Descriptions.Item label="نماینده">{o?.parent_name || 'فروش مستقیم'}</Descriptions.Item>
              <Descriptions.Item label="مشتری از">{faDate(o?.created_at)}</Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card title="خلاصه" loading={org.loading}>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="لایسنس‌های فعال">{faNum(o?.licenses)}</Descriptions.Item>
              <Descriptions.Item label="سریال‌ها">{faNum(o?.serials)}</Descriptions.Item>
              <Descriptions.Item label="کاربران 2FA">{faNum(o?.otp_users)}</Descriptions.Item>
              <Descriptions.Item label="نزدیک‌ترین انقضا"><ExpiryTag date={o?.nearest_expiry ?? null} /></Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
      </Row>
      <Card title="لایسنس‌ها" style={{ marginTop: 16 }} styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id" loading={lics.loading} dataSource={lics.data?.items ?? []} pagination={false} scroll={{ x: true }}
          columns={[
            { title: 'لایسنس', dataIndex: 'code', render: (v, r) => <Link to={`/licenses/${r.id}`}><Mono>{v}</Mono></Link> },
            { title: 'سریال‌ها', dataIndex: 'serials', render: faNum },
            { title: 'کاربران 2FA', dataIndex: 'otp_users', render: faNum },
            { title: 'نصب', dataIndex: 'installation_id', render: (v) => (v ? <Mono>{v}</Mono> : <Tag>فعال‌نشده</Tag>) },
            { title: 'نزدیک‌ترین انقضا', dataIndex: 'nearest_expiry', render: (v, r) => <ExpiryTag date={v} grace={r.grace_days} /> },
            { title: 'وضعیت', dataIndex: 'status', render: statusTag },
          ]}
        />
      </Card>
      <OrgForm kind="customer" open={editing} initial={o} busy={busy} onCancel={() => setEditing(false)} onSubmit={save} />
    </>
  )
}
