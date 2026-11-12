import { roleNames } from '../AppLayout'
import { APITokens } from '../tokens'
import { PageHeader } from '../ui'

export default function Tokens() {
  return (
    <>
      <PageHeader title="توکن‌های API" subtitle="دسترسی سامانه‌های دیگر (CRM، حسابداری، گزارش) به API سرور لایسنس" />
      <APITokens roleNames={roleNames} example="/licenses" />
    </>
  )
}
