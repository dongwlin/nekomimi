import { useState } from 'react'
import { Alert, Button, Card, Form, Input, Typography, message } from 'antd'
import { useNavigate } from 'react-router'
import { rotateSystemPassphrase } from '@/lib/api/auth'
import { useAuthStore } from '../store'

interface SecurityFormValues {
  currentPassphrase: string
  newPassphrase: string
  confirmNewPassphrase: string
}

const ERROR_MESSAGE: Record<string, string> = {
  invalid_request: 'Request payload is invalid. Please check the form.',
  invalid_current_passphrase: 'Current passphrase is incorrect.',
  weak_passphrase: 'New passphrase must be at least 8 characters.',
  unauthorized: 'Your session has expired. Please sign in again.',
  rotate_passphrase_failed: 'Failed to rotate passphrase.',
}

export function SecuritySettingsPage() {
  const navigate = useNavigate()
  const clearTokens = useAuthStore((state) => state.clearTokens)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(values: SecurityFormValues) {
    setSubmitting(true)
    setError(null)
    try {
      await rotateSystemPassphrase(values.currentPassphrase.trim(), values.newPassphrase.trim())
      message.success('Passphrase updated. Please sign in again.')
      clearTokens()
      await navigate('/login', { replace: true })
    } catch (submitError) {
      const code = submitError instanceof Error ? submitError.message : 'rotate_passphrase_failed'
      setError(ERROR_MESSAGE[code] ?? 'Failed to rotate passphrase. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Card bordered={false} style={{ borderRadius: 14 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        Security Settings
      </Typography.Title>
      <Typography.Paragraph type="secondary">
        Rotate the system passphrase. Current sessions will be revoked immediately after success.
      </Typography.Paragraph>

      {error ? (
        <Alert
          type="error"
          showIcon
          message={error}
          style={{ marginBottom: 16 }}
          closable
          onClose={() => setError(null)}
        />
      ) : null}

      <Form<SecurityFormValues> layout="vertical" requiredMark={false} onFinish={onSubmit} disabled={submitting}>
        <Form.Item
          label="Current passphrase"
          name="currentPassphrase"
          rules={[{ required: true, message: 'Please enter current passphrase' }]}
        >
          <Input.Password autoComplete="off" placeholder="Current passphrase" />
        </Form.Item>
        <Form.Item
          label="New passphrase"
          name="newPassphrase"
          rules={[
            { required: true, message: 'Please enter new passphrase' },
            { min: 8, message: 'New passphrase must be at least 8 characters' },
          ]}
        >
          <Input.Password autoComplete="new-password" placeholder="At least 8 characters" />
        </Form.Item>
        <Form.Item
          label="Confirm new passphrase"
          name="confirmNewPassphrase"
          dependencies={['newPassphrase']}
          rules={[
            { required: true, message: 'Please confirm new passphrase' },
            ({ getFieldValue }) => ({
              validator(_, value: string) {
                if (!value || getFieldValue('newPassphrase') === value) {
                  return Promise.resolve()
                }
                return Promise.reject(new Error('The two passphrases do not match'))
              },
            }),
          ]}
        >
          <Input.Password autoComplete="new-password" placeholder="Confirm new passphrase" />
        </Form.Item>
        <Form.Item style={{ marginBottom: 0 }}>
          <Button type="primary" htmlType="submit" loading={submitting}>
            Rotate passphrase
          </Button>
        </Form.Item>
      </Form>
    </Card>
  )
}
