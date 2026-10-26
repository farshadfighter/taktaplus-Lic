import { Form, Input, Modal, Select } from 'antd'
import { useEffect } from 'react'
import type { Org } from '../api'
import { useFetch } from '../useFetch'

export type OrgFormValues = {
  name: string
  legal_name: string
  national_id: string
  parent_id: number | null
  tier: string
  contact: { tech_name: string; tech_phone: string; tech_email: string; finance_name: string }
}

// OrgForm edits a customer or reseller in a modal.
export default function OrgForm({ kind, open, initial, onCancel, onSubmit, busy }: {
  kind: 'customer' | 'reseller'
  open: boolean
  initial?: Org | null
  busy: boolean
  onCancel: () => void
  onSubmit: (v: OrgFormValues) => void
}) {
  const [form] = Form.useForm<OrgFormValues>()
  const resellers = useFetch<{ items: Org[] }>(kind === 'customer' && open ? '/resellers' : null)

  useEffect(() => {
    if (open) form.setFieldsValue(initial ?? { name: '', legal_name: '', national_id: '', parent_id: null, tier: '', contact: { tech_name: '', tech_phone: '', tech_email: '', finance_name: '' } })
  }, [open, initial, form])

  return (
    <Modal
      open={open} onCancel={onCancel} onOk={() => form.submit()} confirmLoading={busy} destroyOnHidden
      title={initial ? 'ویرایش' : kind === 'customer' ? 'مشتری جدید' : 'نماینده‌ی جدید'} okText="ذخیره" cancelText="انصراف"
    >
      <Form form={form} layout="vertical" onFinish={onSubmit} requiredMark={false}>
        <Form.Item name="name" label="نام" rules={[{ required: true, message: 'نام را وارد کنید' }]}><Input /></Form.Item>
        <Form.Item name="legal_name" label="نام حقوقی"><Input /></Form.Item>
        <Form.Item name="national_id" label="شناسه‌ی ملی"><Input dir="ltr" /></Form.Item>
        {kind === 'customer' && (
          <Form.Item name="parent_id" label="نماینده">
            <Select allowClear placeholder="فروش مستقیم" loading={resellers.loading}
              options={(resellers.data?.items ?? []).map((r) => ({ value: r.id, label: `${r.name} (${r.code})` }))} />
          </Form.Item>
        )}
        {kind === 'reseller' && (
          <Form.Item name="tier" label="سطح">
            <Select allowClear options={[{ value: 'gold', label: 'طلایی' }, { value: 'silver', label: 'نقره‌ای' }, { value: 'bronze', label: 'برنزی' }]} />
          </Form.Item>
        )}
        <Form.Item name={['contact', 'tech_name']} label="رابط فنی"><Input /></Form.Item>
        <Form.Item name={['contact', 'tech_phone']} label="تلفن رابط فنی"><Input dir="ltr" /></Form.Item>
        <Form.Item name={['contact', 'tech_email']} label="ایمیل رابط فنی"><Input dir="ltr" /></Form.Item>
        <Form.Item name={['contact', 'finance_name']} label="رابط مالی"><Input /></Form.Item>
      </Form>
    </Modal>
  )
}
