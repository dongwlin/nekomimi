import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Line } from '@ant-design/plots'
import {
  Alert,
  Card,
  Col,
  Descriptions,
  Flex,
  Row,
  Skeleton,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd'
import { PageHeader } from '@/components/PageHeader'
import { getDashboardOverview } from '@/lib/api/dashboard'
import type {
  MetricsOverview,
  MetricsOverviewHourly,
  MetricsOverviewTypeItem,
} from '@/lib/api/generated'

const POLL_INTERVAL_MS = 10_000
const EMPTY_TYPE_ROWS: TypeRow[] = []

interface TypeRow {
  key: string
  type: string
  count: number
  ratio: number
}

interface TrendRow {
  hour: string
  metric: 'Received' | 'Sent' | 'Failed'
  value: number
}

function asNumber(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function formatDateTime(value?: string) {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function formatDuration(seconds?: number) {
  const total = Math.max(0, Math.floor(asNumber(seconds)))
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const secs = total % 60
  const dayPart = days > 0 ? `${days}d ` : ''
  return `${dayPart}${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
}

function buildTopTypeRows(items: MetricsOverviewTypeItem[] | undefined): TypeRow[] {
  if (!items || items.length === 0) {
    return []
  }

  const normalized = items
    .map((item) => ({
      type: item.type ?? 'unknown',
      count: asNumber(item.count),
      ratio: typeof item.ratio === 'number' ? item.ratio : 0,
    }))
    .sort((a, b) => b.count - a.count)

  if (normalized.length <= 10) {
    return normalized.map((item) => ({
      key: item.type,
      ...item,
    }))
  }

  const top = normalized.slice(0, 10)
  const rest = normalized.slice(10)
  const restCount = rest.reduce((sum, item) => sum + item.count, 0)
  const restRatio = rest.reduce((sum, item) => sum + item.ratio, 0)

  return [
    ...top.map((item) => ({
      key: item.type,
      ...item,
    })),
    {
      key: 'other',
      type: 'other',
      count: restCount,
      ratio: restRatio,
    },
  ]
}

function buildTrendRows(points: MetricsOverviewHourly[] | undefined): TrendRow[] {
  if (!points || points.length === 0) {
    return []
  }
  return points.flatMap((point) => [
    {
      hour: point.hour ?? '--:00',
      metric: 'Received' as const,
      value: asNumber(point.received),
    },
    {
      hour: point.hour ?? '--:00',
      metric: 'Sent' as const,
      value: asNumber(point.sent),
    },
    {
      hour: point.hour ?? '--:00',
      metric: 'Failed' as const,
      value: asNumber(point.failed),
    },
  ])
}

export function DashboardPage() {
  const [overview, setOverview] = useState<MetricsOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const inFlightRef = useRef(false)

  const loadOverview = useCallback(async () => {
    if (inFlightRef.current) {
      return
    }
    inFlightRef.current = true
    try {
      const data = await getDashboardOverview()
      setOverview(data)
      setError(null)
    } catch (loadError) {
      if (loadError instanceof Error) {
        setError(loadError.message)
      } else {
        setError('Failed to load dashboard overview.')
      }
    } finally {
      inFlightRef.current = false
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadOverview()
    const timer = window.setInterval(() => {
      void loadOverview()
    }, POLL_INTERVAL_MS)
    return () => {
      window.clearInterval(timer)
    }
  }, [loadOverview])

  const inboundRows = useMemo(() => buildTopTypeRows(overview?.today_inbound_types), [overview?.today_inbound_types])
  const outboundRows = useMemo(() => buildTopTypeRows(overview?.today_outbound_types), [overview?.today_outbound_types])
  const failedRows = useMemo(() => buildTopTypeRows(overview?.today_failed_types), [overview?.today_failed_types])
  const trendRows = useMemo(() => buildTrendRows(overview?.hourly_trend), [overview?.hourly_trend])

  const typeColumns = useMemo(
    () => [
      {
        title: 'Type',
        dataIndex: 'type',
        key: 'type',
      },
      {
        title: 'Count',
        dataIndex: 'count',
        key: 'count',
        width: 110,
      },
      {
        title: 'Ratio',
        dataIndex: 'ratio',
        key: 'ratio',
        width: 110,
        render: (ratio: number) => `${(ratio * 100).toFixed(1)}%`,
      },
    ],
    [],
  )

  if (loading && !overview) {
    return (
      <Card bordered={false} style={{ borderRadius: 14 }}>
        <PageHeader title="Dashboard" description="Runtime status and traffic telemetry." />
        <Skeleton active paragraph={{ rows: 12 }} />
      </Card>
    )
  }

  const runtime = overview?.runtime
  const kpi = overview?.kpi

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card bordered={false} style={{ borderRadius: 14 }}>
        <PageHeader title="Dashboard" description="Runtime status, traffic breakdown, and hourly trend." />
        {error ? (
          <Alert
            type="error"
            showIcon
            message={error}
            style={{ marginBottom: 16 }}
          />
        ) : null}

        <Row gutter={[12, 12]}>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Process Uptime" value={formatDuration(runtime?.uptime_seconds)} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Bot Uptime" value={formatDuration(runtime?.bot_uptime_seconds)} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Today Active Sessions" value={asNumber(kpi?.today_active_sessions)} />
            </Card>
          </Col>

          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Today Received" value={asNumber(kpi?.today_received_total)} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Today Sent" value={asNumber(kpi?.today_sent_total)} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Today Failed" value={asNumber(kpi?.today_failed_total)} />
            </Card>
          </Col>

          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Total Received" value={asNumber(kpi?.total_received_total)} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Total Sent" value={asNumber(kpi?.total_sent_total)} />
            </Card>
          </Col>
          <Col xs={24} sm={12} lg={8}>
            <Card size="small">
              <Statistic title="Total Failed" value={asNumber(kpi?.total_failed_total)} />
            </Card>
          </Col>
        </Row>

        <Descriptions
          size="small"
          column={1}
          style={{ marginTop: 16 }}
          items={[
            {
              key: 'processStartedAt',
              label: 'Process Started',
              children: formatDateTime(runtime?.process_started_at),
            },
            {
              key: 'botStartedAt',
              label: 'Bot Started',
              children: formatDateTime(runtime?.bot_started_at),
            },
            {
              key: 'botConnectedAt',
              label: 'Bot Connected',
              children: formatDateTime(runtime?.bot_connected_at),
            },
            {
              key: 'lastReceivedAt',
              label: 'Last Received',
              children: formatDateTime(kpi?.last_received_at),
            },
            {
              key: 'lastSentAt',
              label: 'Last Sent',
              children: formatDateTime(kpi?.last_sent_at),
            },
            {
              key: 'lastFailedAt',
              label: 'Last Failed',
              children: formatDateTime(kpi?.last_failed_at),
            },
            {
              key: 'timezone',
              label: 'Timezone',
              children: overview?.timezone ?? '-',
            },
            {
              key: 'llmStatus',
              label: 'LLM',
              children: (
                <Flex align="center" gap={8}>
                  <Tag color={kpi?.llm_enabled ? 'green' : 'default'}>{kpi?.llm_enabled ? 'Enabled' : 'Disabled'}</Tag>
                  <Typography.Text type="secondary">{kpi?.llm_model ?? '-'}</Typography.Text>
                </Flex>
              ),
            },
          ]}
        />
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card title="Inbound Types (Today)">
            <Table<TypeRow>
              size="small"
              rowKey="key"
              pagination={false}
              columns={typeColumns}
              dataSource={inboundRows.length > 0 ? inboundRows : EMPTY_TYPE_ROWS}
              locale={{ emptyText: 'No data' }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="Outbound Types (Today)">
            <Table<TypeRow>
              size="small"
              rowKey="key"
              pagination={false}
              columns={typeColumns}
              dataSource={outboundRows.length > 0 ? outboundRows : EMPTY_TYPE_ROWS}
              locale={{ emptyText: 'No data' }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="Failed Types (Today)">
            <Table<TypeRow>
              size="small"
              rowKey="key"
              pagination={false}
              columns={typeColumns}
              dataSource={failedRows.length > 0 ? failedRows : EMPTY_TYPE_ROWS}
              locale={{ emptyText: 'No data' }}
            />
          </Card>
        </Col>
      </Row>

      <Card title="Hourly Trend (Received / Sent / Failed)">
        <Line
          height={300}
          data={trendRows}
          xField="hour"
          yField="value"
          seriesField="metric"
          legend={{ position: 'top' }}
          color={['#1677ff', '#52c41a', '#ff4d4f']}
          point={{ size: 2 }}
          smooth
          yAxis={{ title: false }}
          xAxis={{ title: false }}
          tooltip={{
            formatter: (datum: TrendRow) => ({
              name: datum.metric,
              value: String(datum.value),
            }),
          }}
        />
      </Card>
    </Space>
  )
}
