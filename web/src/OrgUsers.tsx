import { useState } from 'react'
import { App, Button, Form, Input, Modal, Switch, Table, Tag } from 'antd'
import { api, type PortalUser } from './api'
import { faDateTime } from './format'
import { useFetch } from './useFetch'

// OrgUsers lists and adds the portal users of a customer or reseller.
// base is the API path of the organization's users.
export default function OrgUsers({ base, writable }: { base: string; writable: boolean }) {
  const { message } = App.useApp()
  const list = useFetch<{ items: PortalUser[] }>(base)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [form] = Form.useForm()
  return (
    <>
      <Table<PortalUser>
        size="small" rowKey="id" pagination={false} loading={list.loading} dataSource={list.data?.items ?? []}
        locale={{ emptyText: 'کاربری برای پورتال تعریف نشده است' }}
        columns={[
          { title: 'ایمیل', dataIndex: 'email', render: (v) => <span dir="ltr">{v}</span> },
          { title: 'نام', dataIndex: 'name' },
          { title: 'ورود دو مرحله‌ای', dataIndex: 'totp_enabled', render: (v) => (v ? <Tag color="green">فعال</Tag> : <Tag>در اولین ورود</Tag>) },
          { title: 'آخرین ورود', dataIndex: 'last_login_at', render: (v) => faDateTime(v) },
          {
            title: 'فعال', dataIndex: 'active',
            render: (v: boolean, u) => <Switch size="small" checked={v} disabled={!writable} onChange={async (on) => {
              try {
                await api.put(`${base}/${u.id}`, { active: on })
                void list.reload()
              } catch (e) {
                message.error((e as Error).message)
              }
            }} />,
          },
        ]}
        footer={writable ? () => <Button size="small" onClick={() => { form.resetFields(); setOpen(true) }}>+ کاربر پورتال</Button> : undefined}
      />
      <Modal open={open} title="کاربر پورتال" okText="ایجاد" cancelText="انصراف" confirmLoading={busy} onCancel={() => setOpen(false)} onOk={() => form.submit()} destroyOnHidden>
        <Form form={form} layout="vertical" requiredMark={false} onFinish={async (v) => {
          setBusy(true)
          try {
            await api.post(base, v)
            message.success('کاربر ایجاد شد؛ رمز اولیه را به او بدهید. در اولین ورود ورود دو مرحله‌ای را فعال می‌کند.')
            setOpen(false)
            void list.reload()
          } catch (e) {
            message.error((e as Error).message)
          } finally {
            setBusy(false)
          }
        }}>
          <Form.Item name="email" label="ایمیل" rules={[{ required: true, type: 'email', message: 'ایمیل معتبر' }]}><Input dir="ltr" /></Form.Item>
          <Form.Item name="name" label="نام" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="password" label="رمز اولیه" rules={[{ required: true, min: 12, message: 'حداقل ۱۲ کاراکتر' }]}><Input.Password dir="ltr" autoComplete="new-password" /></Form.Item>
        </Form>
      </Modal>
    </>
  )
}
