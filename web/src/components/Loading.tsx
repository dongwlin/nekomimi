import { Space, Spin, Typography } from 'antd'

interface LoadingProps {
  label?: string
}

export function Loading({ label = 'Loading...' }: LoadingProps) {
  return (
    <Space size={10}>
      <Spin size="small" />
      <Typography.Text type="secondary">{label}</Typography.Text>
    </Space>
  )
}
