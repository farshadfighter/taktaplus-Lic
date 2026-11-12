import { useState, type ReactNode } from 'react'
import { Form, Input, Modal, Tag, Typography } from 'antd'
import { daysUntil, faDate } from './format'

export function PageHeader({ title, subtitle, extra }: { title: ReactNode; subtitle?: ReactNode; extra?: ReactNode }) {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 20 }}>
      <div>
        <Typography.Title level={3} style={{ margin: 0 }}>{title}</Typography.Title>
        {subtitle && <Typography.Text type="secondary">{subtitle}</Typography.Text>}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>{extra}</div>
    </div>
  )
}

export function Mono({ children }: { children: ReactNode }) {
  return <span dir="ltr" style={{ fontFamily: 'ui-monospace, Consolas, monospace', unicodeBidi: 'isolate' }}>{children}</span>
}

// ExpiryTag colors an expiry date by how close it is, counting the grace
// period as a warning.
export function ExpiryTag({ date, grace = 14 }: { date: string | null; grace?: number }) {
  const d = daysUntil(date)
  if (d === null) return <Tag>—</Tag>
  if (d < -grace) return <Tag color="red">منقضی · {faDate(date)}</Tag>
  if (d < 0) return <Tag color="orange">در مهلت · {faDate(date)}</Tag>
  if (d <= 30) return <Tag color="gold">{faDate(date)}</Tag>
  return <Tag color="green">{faDate(date)}</Tag>
}

export const statusTag = (s: string) =>
  s === 'active' ? <Tag color="green">فعال</Tag> : s === 'revoked' ? <Tag color="red">باطل‌شده</Tag> : s === 'released' ? <Tag>آزادشده</Tag> : <Tag>{s}</Tag>

export const warningLabels: Record<string, string> = {
  clock_rollback: 'عقب‌رفتن ساعت',
  fingerprint_changed: 'تغییر سخت‌افزار',
  over_usage: 'مصرف بیش از ظرفیت',
}

export function bytesText(n: number): string {
  if (!n) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

// useReauth asks for the password and current authenticator code
// before a sensitive action; the server checks both again.
export function useReauth() {
  const [state, setState] = useState<{ title: string; body?: ReactNode; okText: string; run: (v: { password: string; code: string }) => Promise<void> } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [form] = Form.useForm()

  const ask = (title: string, okText: string, run: (v: { password: string; code: string }) => Promise<void>, body?: ReactNode) => {
    form.resetFields()
    setError(null)
    setState({ title, okText, run, body })
  }

  const modal = (
    <Modal
      open={state !== null}
      title={state?.title}
      okText={state?.okText}
      okButtonProps={{ danger: true, loading: busy }}
      cancelText="انصراف"
      onCancel={() => setState(null)}
      onOk={() => form.submit()}
      destroyOnHidden
    >
      {state?.body}
      <Typography.Paragraph type="secondary">برای ادامه، رمز عبور و کد Authenticator خود را وارد کنید.</Typography.Paragraph>
      {error && <Typography.Paragraph type="danger">{error}</Typography.Paragraph>}
      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        onFinish={async (v) => {
          if (!state) return
          setBusy(true)
          setError(null)
          try {
            await state.run({ password: v.password, code: String(v.code).trim() })
            setState(null)
          } catch (e) {
            setError((e as Error).message)
          } finally {
            setBusy(false)
          }
        }}
      >
        <Form.Item name="password" label="رمز عبور" rules={[{ required: true, message: 'رمز را وارد کنید' }]}>
          <Input.Password dir="ltr" autoComplete="current-password" />
        </Form.Item>
        <Form.Item name="code" label="کد ۶ رقمی" rules={[{ required: true, pattern: /^\s*\d{6}\s*$/, message: 'کد ۶ رقمی را وارد کنید' }]}>
          <Input dir="ltr" inputMode="numeric" autoComplete="one-time-code" style={{ letterSpacing: 6, textAlign: 'center' }} />
        </Form.Item>
      </Form>
    </Modal>
  )
  return { ask, modal }
}
