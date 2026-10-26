import { Card, Col, List, Row, Statistic, Table, Tag } from 'antd'
import { Link } from 'react-router'
import type { InstallationRow, LicenseSummary, Org } from '../api'
import { faNum } from '../format'
import { ExpiryTag, Mono, PageHeader, warningLabels } from '../ui'
import { useFetch } from '../useFetch'

export default function Dashboard() {
  const customers = useFetch<{ items: Org[] }>('/customers')
  const licenses = useFetch<{ items: LicenseSummary[] }>('/licenses?status=active')
  const expiring = useFetch<{ items: LicenseSummary[] }>('/licenses?status=active&expiring_in=30')
  const installs = useFetch<{ items: InstallationRow[] }>('/installations')

  const lics = licenses.data?.items ?? []
  const serials = lics.reduce((n, l) => n + l.serials, 0)
  const otp = lics.reduce((n, l) => n + l.otp_users, 0)
  const weekAgo = Date.now() - 7 * 86_400_000
  const silent = (installs.data?.items ?? []).filter(
    (i) => i.status === 'active' && i.mode === 'online' && (!i.last_heartbeat_at || Date.parse(i.last_heartbeat_at) < weekAgo),
  )
  const flagged = (installs.data?.items ?? []).filter((i) => i.warnings.length > 0)

  return (
    <>
      <PageHeader title="داشبورد" subtitle="لایسنس‌ها، انقضاها و وضعیت نصب‌ها" />
      <Row gutter={[16, 16]}>
        <Col xs={12} lg={6}><Card><Statistic title="مشتریان" value={faNum(customers.data?.items.length ?? 0)} /></Card></Col>
        <Col xs={12} lg={6}><Card><Statistic title="سریال‌های تحت اشتراک" value={faNum(serials)} /></Card></Col>
        <Col xs={12} lg={6}><Card><Statistic title="کاربران 2FA" value={faNum(otp)} /></Card></Col>
        <Col xs={12} lg={6}><Card><Statistic title="انقضا تا ۳۰ روز" value={faNum(expiring.data?.items.length ?? 0)} /></Card></Col>
      </Row>
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} xl={15}>
          <Card title="انقضاهای ۳۰ روز آینده" extra={<Link to="/licenses">همه</Link>}>
            <Table
              rowKey="id" size="small" pagination={false} loading={expiring.loading} scroll={{ x: true }}
              dataSource={expiring.data?.items ?? []}
              columns={[
                { title: 'مشتری', dataIndex: 'customer_name', render: (v, r) => <Link to={`/customers/${r.customer_id}`}>{v}</Link> },
                { title: 'لایسنس', dataIndex: 'code', render: (v, r) => <Link to={`/licenses/${r.id}`}><Mono>{v}</Mono></Link> },
                { title: 'سریال‌ها', dataIndex: 'serials', render: faNum },
                { title: 'نزدیک‌ترین انقضا', dataIndex: 'nearest_expiry', render: (v, r) => <ExpiryTag date={v} grace={r.grace_days} /> },
                { title: 'نماینده', dataIndex: 'reseller_name', render: (v) => v || 'مستقیم' },
              ]}
            />
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card title="نیازمند بررسی" extra={<Link to="/installations">نصب‌ها</Link>}>
            <List
              loading={installs.loading}
              locale={{ emptyText: 'موردی نیست' }}
              dataSource={[
                ...flagged.map((i) => ({ i, tag: <Tag color="red">{i.warnings.map((w) => warningLabels[w] ?? w).join('، ')}</Tag> })),
                ...silent.map((i) => ({ i, tag: <Tag color="orange">بدون Heartbeat بیش از ۷ روز</Tag> })),
              ]}
              renderItem={({ i, tag }) => (
                <List.Item>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div>{tag}</div>
                    <span>{i.customer_name ?? '—'} · <Mono>{i.license_code ?? i.id}</Mono></span>
                  </div>
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
    </>
  )
}
