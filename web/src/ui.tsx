import type { ReactNode } from 'react'
import { Tag, Typography } from 'antd'
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
