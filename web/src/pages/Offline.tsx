import { useState } from 'react'
import { Alert, App, Button, Card, Col, Descriptions, Input, List, Row, Tag, Typography, Upload } from 'antd'
import { Link } from 'react-router'
import { api, type Inspection } from '../api'
import { saveText } from '../download'
import { faDateTime, faNum } from '../format'
import { Mono, PageHeader } from '../ui'

const checkNames: Record<string, string> = {
  request_fresh: 'درخواست تازه است (کمتر از ۷ روز)',
  not_replayed: 'درخواست قبلاً استفاده نشده',
  license_found: 'کد فعال‌سازی / لایسنس معتبر',
  license_active: 'لایسنس باطل نشده',
  not_bound_elsewhere: 'لایسنس به نصب دیگری متصل نیست',
  bound_to_this_installation: 'لایسنس به همین نصب متصل است',
  hardware_matches: 'سخت‌افزار تغییر نکرده',
  installation_active: 'نصب آزاد نشده',
}

export default function Offline() {
  const [token, setToken] = useState('')
  const [inspection, setInspection] = useState<Inspection | null>(null)
  const [issued, setIssued] = useState<{ license: string; license_id: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const { message } = App.useApp()

  async function inspect(text: string) {
    setToken(text.trim())
    setIssued(null)
    setBusy(true)
    try {
      setInspection(await api.post<Inspection>('/activations/inspect', { request: text.trim() }))
    } catch (e) {
      setInspection(null)
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function issue() {
    setBusy(true)
    try {
      setIssued(await api.post('/activations/offline', { request: token }))
    } catch (e) {
      message.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const reset = () => {
    setToken('')
    setInspection(null)
    setIssued(null)
  }
  const req = inspection?.request

  return (
    <>
      <PageHeader title="فعال‌سازی آفلاین" subtitle="فایل درخواست (.tpreq) را بارگذاری کنید و فایل لایسنس (.tplic) تحویل دهید" />
      {!inspection && (
        <Card title="۱. فایل درخواست">
          <Upload.Dragger accept=".tpreq,.txt" showUploadList={false} beforeUpload={(f) => { void f.text().then(inspect); return false }}>
            <p style={{ color: '#900A3C', margin: 16 }}>فایل request.tpreq را این‌جا رها کنید یا برای انتخاب کلیک کنید</p>
          </Upload.Dragger>
          <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>یا متن درخواست را بچسبانید:</Typography.Paragraph>
          <Input.TextArea rows={3} dir="ltr" value={token} onChange={(e) => setToken(e.target.value)} placeholder="TPREQ1.…" />
          <Button style={{ marginTop: 8 }} disabled={!token.trim()} loading={busy} onClick={() => void inspect(token)}>بررسی</Button>
        </Card>
      )}

      {inspection && req && (
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title="۲. محتوای درخواست">
              <Descriptions column={1} size="small">
                <Descriptions.Item label="نوع">{req.type === 'activate' ? 'فعال‌سازی' : 'به‌روزرسانی'}</Descriptions.Item>
                <Descriptions.Item label="لایسنس">{inspection.license_id ? <Link to={`/licenses/${inspection.license_id}`}><Mono>{inspection.license_code}</Mono></Link> : '—'} {inspection.customer && `· ${inspection.customer}`}</Descriptions.Item>
                <Descriptions.Item label="شناسه‌ی نصب"><Mono>{req.installation_id}</Mono></Descriptions.Item>
                <Descriptions.Item label="نسخه"><Mono>{req.product_version}</Mono></Descriptions.Item>
                <Descriptions.Item label="زمان ساخت">{faDateTime(req.created_at)}</Descriptions.Item>
                {req.usage && <Descriptions.Item label="مصرف فعلی">{faNum(req.usage.devices?.length ?? 0)} دستگاه · {faNum(req.usage.otp_users)} کاربر 2FA</Descriptions.Item>}
              </Descriptions>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title="۳. بررسی‌ها">
              <List size="small" dataSource={inspection.checks} renderItem={(c) => (
                <List.Item extra={c.ok ? <Tag color="green">تأیید</Tag> : <Tag color="red">رد</Tag>}>{checkNames[c.name] ?? c.name}</List.Item>
              )} />
              {!issued && (
                <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
                  <Button type="primary" disabled={!inspection.can_issue} loading={busy} onClick={issue}>صدور فایل لایسنس</Button>
                  <Button onClick={reset}>درخواست دیگر</Button>
                </div>
              )}
            </Card>
          </Col>
        </Row>
      )}

      {issued && (
        <Card style={{ marginTop: 16, borderColor: '#B7E1C4', background: '#F4FBF6' }} title="۴. فایل لایسنس آماده است">
          <Alert type="success" showIcon title={<>لایسنس <Mono>{issued.license_id}</Mono> به این نصب متصل شد. فایل فقط روی همان نصب کار می‌کند.</>} style={{ marginBottom: 16 }} />
          <div style={{ display: 'flex', gap: 8 }}>
            <Button type="primary" onClick={() => saveText(`${issued.license_id}.tplic`, issued.license + '\n')}>دانلود license.tplic</Button>
            <Button onClick={reset}>درخواست دیگر</Button>
          </div>
        </Card>
      )}
    </>
  )
}
