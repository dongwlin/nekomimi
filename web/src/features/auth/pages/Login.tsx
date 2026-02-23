import { useMemo, useState } from 'react'
import { Alert, Button, Card, Form, Input } from 'antd'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { PageHeader } from '@/components/PageHeader'
import { loginWithPassphrase } from '@/lib/api/auth'
import { useAuthStore } from '../store'

interface RedirectState {
  from?: {
    pathname?: string
  }
}

interface LoginFormValues {
  passphrase: string
}

export function LoginPage() {
  const accessToken = useAuthStore((state) => state.accessToken)
  const navigate = useNavigate()
  const location = useLocation()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const redirectPath = useMemo(() => {
    const state = location.state as RedirectState | null
    return state?.from?.pathname ?? '/dashboard'
  }, [location.state])

  if (accessToken) {
    return <Navigate to="/dashboard" replace />
  }

  async function handleFinish(values: LoginFormValues) {
    setSubmitting(true)
    setError(null)

    try {
      await loginWithPassphrase(values.passphrase.trim())
      await navigate(redirectPath, { replace: true })
    } catch (submitError) {
      if (submitError instanceof Error) {
        setError(submitError.message)
      } else {
        setError('Login failed. Please try again.')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Card
      style={{ width: 'min(100%, 420px)', borderRadius: 14 }}
      styles={{
        body: {
          padding: 24,
        },
      }}
    >
      <PageHeader title="Sign in" description="Enter the system passphrase to exchange an auth token." />

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

      <Form<LoginFormValues>
        layout="vertical"
        requiredMark={false}
        onFinish={handleFinish}
        disabled={submitting}
      >
        <Form.Item
          label="Passphrase"
          name="passphrase"
          rules={[
            {
              required: true,
              message: 'Please enter passphrase',
            },
          ]}
        >
          <Input.Password placeholder="Input your passphrase" autoComplete="off" />
        </Form.Item>

        <Form.Item style={{ marginBottom: 0 }}>
          <Button type="primary" htmlType="submit" block loading={submitting}>
            Sign in
          </Button>
        </Form.Item>
      </Form>
    </Card>
  )
}
