import { Alert, Card, Col, Row, Statistic, Typography } from 'antd'
import { PageHeader } from '@/components/PageHeader'

export function DashboardPage() {
  return (
    <Card bordered={false} style={{ borderRadius: 14 }}>
      <PageHeader title="Dashboard" description="Token-authenticated admin workspace." />

      <Alert
        type="info"
        showIcon
        message="Current session is authenticated with PASETO bearer token."
        style={{ marginBottom: 20 }}
      />

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={8}>
          <Card size="small">
            <Statistic title="Token Verifications" value={48} />
          </Card>
        </Col>
        <Col xs={24} sm={8}>
          <Card size="small">
            <Statistic title="Messages Today" value={1532} />
          </Card>
        </Col>
        <Col xs={24} sm={8}>
          <Card size="small">
            <Statistic title="Active Groups" value={12} />
          </Card>
        </Col>
      </Row>

      <Typography.Paragraph type="secondary" style={{ marginTop: 20, marginBottom: 0 }}>
        You can continue wiring real API calls in feature modules without changing the app shell.
      </Typography.Paragraph>
    </Card>
  )
}
