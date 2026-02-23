import { Typography } from 'antd'

interface PageHeaderProps {
  title: string
  description?: string
}

export function PageHeader({ title, description }: PageHeaderProps) {
  return (
    <header style={{ marginBottom: 20 }}>
      <Typography.Title level={2} style={{ margin: 0, fontSize: 28 }}>
        {title}
      </Typography.Title>
      {description ? (
        <Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>
          {description}
        </Typography.Paragraph>
      ) : null}
    </header>
  )
}
