import { useState } from 'react'
import { Alert, Button, Card, Col, DatePicker, Form, Input, Modal, Progress, Radio, Row, Select, Space, Table, Tabs, Tag, Typography, Upload, App as AntApp } from 'antd'
import dayjs from 'dayjs'
import { api, type ManifestInfo, type RepoPackage } from '../api'
import { useAuth, can } from '../auth'
import { faDateTime, faNum } from '../format'
import { Mono, PageHeader } from '../ui'
import { useFetch } from '../useFetch'

const types: Record<string, string[]> = {
  fortigate: ['ips_db', 'ips_engine', 'av_db', 'av_engine', 'appctrl_db', 'botnet_db', 'isdb', 'ot_db', 'firmware'],
  fortiweb: ['waf_signature', 'av_db', 'ip_reputation', 'geo_db', 'firmware'],
}

export const typeLabel: Record<string, string> = {
  ips_db: 'پایگاه IPS', ips_engine: 'موتور IPS', av_db: 'پایگاه آنتی‌ویروس', av_engine: 'موتور آنتی‌ویروس', appctrl_db: 'کنترل برنامه',
  botnet_db: 'Botnet', isdb: 'Internet Service DB', ot_db: 'OT', waf_signature: 'امضای WAF', ip_reputation: 'اعتبار IP', geo_db: 'Geo IP', firmware: 'فرم‌ور',
}

const statusTag = (s: string, inManifest: boolean) =>
  s === 'published' ? <Tag color={inManifest ? 'green' : 'gold'}>{inManifest ? 'منتشرشده' : 'آماده‌ی انتشار'}</Tag>
    : s === 'withdrawn' ? <Tag color="red">بازپس‌گرفته</Tag> : <Tag>پیش‌نویس</Tag>

const sizeText = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.ceil(n / 1024)} KB`)

type MetaForm = {
  file?: string
  platform: string
  type: string
  version: string
  models: string[]
  fw_min?: string
  fw_max?: string
  upgrade_from: string[]
  notes?: string
  released_at: dayjs.Dayjs
}

function MetaFields({ platform, type }: { platform: string; type: string }) {
  return (
    <>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="platform" label="پلتفرم"><Radio.Group optionType="button" options={[{ value: 'fortigate', label: 'FortiGate' }, { value: 'fortiweb', label: 'FortiWeb' }]} /></Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="type" label="نوع" rules={[{ required: true }]}>
            <Select options={(types[platform] ?? []).map((t) => ({ value: t, label: typeLabel[t] ?? t }))} />
          </Form.Item>
        </Col>
        <Col span={12}><Form.Item name="version" label="نسخه" rules={[{ required: true }]}><Input dir="ltr" placeholder="28.00950" /></Form.Item></Col>
        <Col span={12}><Form.Item name="released_at" label="تاریخ انتشار"><DatePicker style={{ width: '100%' }} /></Form.Item></Col>
        <Col span={24}>
          <Form.Item name="models" label="مدل‌های سازگار" extra="خالی یا * یعنی همه‌ی مدل‌ها؛ مثل FG100F">
            <Select mode="tags" tokenSeparators={[',', ' ']} />
          </Form.Item>
        </Col>
        <Col span={12}><Form.Item name="fw_min" label="حداقل فرم‌ور"><Input dir="ltr" placeholder="7.0.0" /></Form.Item></Col>
        <Col span={12}><Form.Item name="fw_max" label="حداکثر فرم‌ور"><Input dir="ltr" placeholder="7.6.99" /></Form.Item></Col>
        {type === 'firmware' && (
          <Col span={24}>
            <Form.Item name="upgrade_from" label="ارتقای مستقیم از نسخه‌های" extra="مثل 7.2 (همه‌ی 7.2.x) یا 7.4.1">
              <Select mode="tags" tokenSeparators={[',', ' ']} />
            </Form.Item>
          </Col>
        )}
        <Col span={24}><Form.Item name="notes" label="توضیحات"><Input.TextArea rows={2} /></Form.Item></Col>
      </Row>
    </>
  )
}

const toMeta = (v: MetaForm) => ({
  file: v.file ?? '', platform: v.platform, type: v.type, version: v.version.trim(), models: v.models ?? [],
  fw_min: v.fw_min ?? '', fw_max: v.fw_max ?? '', upgrade_from: v.type === 'firmware' ? v.upgrade_from ?? [] : [],
  notes: v.notes ?? '', released_at: (v.released_at ?? dayjs()).toISOString(),
})

function UploadModal({ open, onClose }: { open: boolean; onClose: (done: boolean) => void }) {
  const [form] = Form.useForm<MetaForm>()
  const platform = Form.useWatch('platform', form) ?? 'fortigate'
  const type = Form.useWatch('type', form) ?? ''
  const [file, setFile] = useState<File | null>(null)
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const submit = (v: MetaForm) => {
    if (!file) {
      setError('فایل را انتخاب کنید')
      return
    }
    setError(null)
    const body = new FormData()
    body.append('meta', JSON.stringify({ ...toMeta(v), file: v.file || file.name }))
    body.append('file', file)
    // XMLHttpRequest reports upload progress; firmware images are large.
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/v1/packages')
    xhr.setRequestHeader('X-Requested-With', 'taktaplus')
    xhr.upload.onprogress = (e) => e.lengthComputable && setProgress(Math.round((e.loaded / e.total) * 100))
    xhr.onload = () => {
      setProgress(null)
      if (xhr.status === 201) {
        form.resetFields()
        setFile(null)
        onClose(true)
      } else {
        try { setError(JSON.parse(xhr.responseText).error) } catch { setError(xhr.statusText) }
      }
    }
    xhr.onerror = () => { setProgress(null); setError('ارسال فایل ناموفق بود') }
    setProgress(0)
    xhr.send(body)
  }

  return (
    <Modal open={open} title="بارگذاری پکیج یا فرم‌ور" okText="بارگذاری" cancelText="انصراف" width={640}
      okButtonProps={{ loading: progress !== null }} onOk={() => form.submit()} onCancel={() => progress === null && onClose(false)}>
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}
      <Form form={form} layout="vertical" requiredMark={false} onFinish={submit} initialValues={{ platform: 'fortigate', models: ['*'], upgrade_from: [], released_at: dayjs() }}>
        <Form.Item label="فایل">
          <Upload.Dragger maxCount={1} beforeUpload={(f) => { setFile(f); if (!form.getFieldValue('file')) form.setFieldValue('file', f.name); return false }}
            onRemove={() => setFile(null)} fileList={file ? [{ uid: '1', name: file.name, status: 'done' }] : []}>
            <p>فایل را اینجا رها کنید یا کلیک کنید</p>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>پکیج دریافت‌شده از پورتال فورتی‌نت (تا ۴ گیگابایت)</Typography.Text>
          </Upload.Dragger>
        </Form.Item>
        <Form.Item name="file" label="نام فایل در Manifest" rules={[{ required: true }]}><Input dir="ltr" /></Form.Item>
        <MetaFields platform={platform} type={type} />
      </Form>
      {progress !== null && <Progress percent={progress} />}
    </Modal>
  )
}

function EditModal({ pkg, onClose }: { pkg: RepoPackage | null; onClose: (done: boolean) => void }) {
  const [form] = Form.useForm<MetaForm>()
  const platform = Form.useWatch('platform', form) ?? pkg?.platform ?? 'fortigate'
  const type = Form.useWatch('type', form) ?? pkg?.type ?? ''
  const [error, setError] = useState<string | null>(null)
  return (
    <Modal open={pkg !== null} title={`ویرایش ${pkg?.file ?? ''}`} okText="ذخیره" cancelText="انصراف" width={640} destroyOnHidden
      onOk={() => form.submit()} onCancel={() => onClose(false)}
      afterOpenChange={(o) => { if (o && pkg) { setError(null); form.setFieldsValue({ ...pkg, released_at: dayjs(pkg.released_at) }) } }}>
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}
      <Form form={form} layout="vertical" requiredMark={false} onFinish={async (v) => {
        try { await api.put(`/packages/${pkg!.id}`, toMeta(v)); onClose(true) } catch (e) { setError((e as Error).message) }
      }}>
        <Form.Item name="file" label="نام فایل در Manifest" rules={[{ required: true }]}><Input dir="ltr" /></Form.Item>
        <MetaFields platform={platform} type={type} />
      </Form>
    </Modal>
  )
}

export default function Packages() {
  const { message, modal } = AntApp.useApp()
  const { user } = useAuth()
  const canPublish = can(user, 'repo.publish')
  const [platform, setPlatform] = useState('')
  const { data, loading, reload } = useFetch<{ items: RepoPackage[]; manifests: ManifestInfo[]; kid: string }>('/packages?platform=' + platform)
  const [uploading, setUploading] = useState(false)
  const [editing, setEditing] = useState<RepoPackage | null>(null)
  const items = data?.items ?? []
  const pending = items.filter((p) => (p.status === 'published') !== p.in_manifest).length
  const current = data?.manifests[0]

  const setStatus = async (p: RepoPackage, status: string) => {
    try {
      await api.post(`/packages/${p.id}/status`, { status })
      void reload()
    } catch (e) {
      message.error((e as Error).message)
    }
  }
  const publish = () =>
    modal.confirm({
      title: 'انتشار Manifest جدید', okText: 'امضا و انتشار', cancelText: 'انصراف',
      content: 'همه‌ی پکیج‌های «منتشرشده» در Manifest جدید قرار می‌گیرند و با کلید انتشار پکیج امضا می‌شوند. نصب‌ها در همگام‌سازی بعدی آن را دریافت می‌کنند.',
      onOk: async () => {
        try {
          const r = await api.post<ManifestInfo>('/packages/publish')
          message.success(`Manifest با ${faNum(r.package_count)} پکیج منتشر شد`)
          void reload()
        } catch (e) {
          message.error((e as Error).message)
        }
      },
    })

  return (
    <>
      <PageHeader title="مخزن پکیج‌ها" subtitle={current ? `Manifest فعلی: ${faDateTime(current.published_at)} · ${faNum(current.package_count)} پکیج · کلید ${current.kid}` : 'هنوز Manifestی منتشر نشده است'}
        extra={canPublish && (
          <>
            <Button onClick={() => setUploading(true)}>بارگذاری</Button>
            <Button type="primary" onClick={publish} disabled={!data?.kid}>انتشار Manifest{pending > 0 ? ` (${faNum(pending)} تغییر)` : ''}</Button>
          </>
        )} />
      {data && !data.kid && <Alert type="warning" showIcon style={{ marginBottom: 16 }} title="کلید انتشار پکیج تنظیم نشده (TPLIC_PACKAGE_KEY_FILE)؛ انتشار Manifest ممکن نیست." />}
      <Tabs items={[
        {
          key: 'packages', label: 'پکیج‌ها', children: (
            <Card>
              <Radio.Group optionType="button" value={platform} onChange={(e) => setPlatform(e.target.value)} style={{ marginBottom: 16 }}
                options={[{ value: '', label: 'همه' }, { value: 'fortigate', label: 'FortiGate' }, { value: 'fortiweb', label: 'FortiWeb' }]} />
              <Table<RepoPackage> rowKey="id" size="small" loading={loading} dataSource={items} scroll={{ x: 1000 }} pagination={{ pageSize: 30, hideOnSinglePage: true }}
                columns={[
                  { title: 'فایل', render: (_, p) => <div><Mono>{p.file}</Mono><div><Typography.Text type="secondary" style={{ fontSize: 12 }}><Mono>{p.sha256.slice(0, 12)}…</Mono> · {sizeText(p.size)}</Typography.Text></div></div> },
                  { title: 'نوع', render: (_, p) => <span>{p.platform === 'fortigate' ? 'FortiGate' : 'FortiWeb'} · {typeLabel[p.type] ?? p.type}</span> },
                  { title: 'نسخه', render: (_, p) => <Mono>{p.version}</Mono> },
                  {
                    title: 'سازگاری', render: (_, p) => (
                      <Space orientation="vertical" size={0} style={{ fontSize: 12 }}>
                        <span>{p.models.join('، ')}</span>
                        {(p.fw_min || p.fw_max) && <Mono>{p.fw_min || '…'} – {p.fw_max || '…'}</Mono>}
                        {p.upgrade_from.length > 0 && <span>ارتقا از <Mono>{p.upgrade_from.join(', ')}</Mono></span>}
                      </Space>
                    ),
                  },
                  { title: 'وضعیت', render: (_, p) => statusTag(p.status, p.in_manifest) },
                  ...(canPublish ? [{
                    title: '', render: (_: unknown, p: RepoPackage) => (
                      <Space size={0} wrap>
                        {p.status !== 'published' && <Button size="small" type="link" onClick={() => void setStatus(p, 'published')}>انتشار</Button>}
                        {p.status === 'published' && <Button size="small" type="link" danger onClick={() => void setStatus(p, 'withdrawn')}>بازپس‌گیری</Button>}
                        <Button size="small" type="link" onClick={() => setEditing(p)}>ویرایش</Button>
                        <Button size="small" type="link" href={`/api/v1/packages/files/${p.sha256}`}>دانلود</Button>
                      </Space>
                    ),
                  }] : []),
                ]} />
            </Card>
          ),
        },
        {
          key: 'manifests', label: 'تاریخچه‌ی انتشار', children: (
            <Card>
              <Table<ManifestInfo> rowKey="id" size="small" pagination={false} dataSource={data?.manifests ?? []}
                columns={[
                  { title: '#', dataIndex: 'id', render: (v: number) => faNum(v) },
                  { title: 'زمان', render: (_, m) => faDateTime(m.published_at) },
                  { title: 'پکیج‌ها', render: (_, m) => faNum(m.package_count) },
                  { title: 'کلید', render: (_, m) => <Mono>{m.kid}</Mono> },
                  { title: 'توسط', dataIndex: 'published_by' },
                ]} />
            </Card>
          ),
        },
        {
          key: 'bundle', label: 'بسته‌ی آفلاین', children: (
            <Card>
              <Typography.Paragraph>برای مشتریانی که به اینترنت دسترسی ندارند: فایل zip شامل Manifest امضاشده و پکیج‌ها، که در پنل TaktaPlus بارگذاری می‌شود.</Typography.Paragraph>
              <Space wrap>
                <Button href="/api/v1/packages/bundle" disabled={!current}>همه‌ی پکیج‌ها</Button>
                <Button href="/api/v1/packages/bundle?platform=fortigate" disabled={!current}>فقط FortiGate</Button>
                <Button href="/api/v1/packages/bundle?platform=fortiweb" disabled={!current}>فقط FortiWeb</Button>
              </Space>
            </Card>
          ),
        },
      ]} />
      <UploadModal open={uploading} onClose={(done) => { setUploading(false); if (done) { message.success('بارگذاری شد؛ برای انتشار، وضعیت را «انتشار» کنید و Manifest را منتشر کنید'); void reload() } }} />
      <EditModal pkg={editing} onClose={(done) => { setEditing(null); if (done) void reload() }} />
    </>
  )
}
