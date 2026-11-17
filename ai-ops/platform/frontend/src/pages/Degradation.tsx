import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Row,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import { AlertOutlined, LineChartOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { api } from '../api';
import type { AlarmEvent, DegradationOverview } from '../types';

const SEVERITY_COLOR: Record<string, string> = {
  critical: 'red',
  warning: 'orange',
  info: 'blue',
};

const SEVERITY_LABEL: Record<string, string> = {
  critical: '严重',
  warning: '警告',
  info: '提示',
};

function fmtTime(v?: string): string {
  if (!v) return '-';
  return v.replace('T', ' ').slice(0, 19);
}

/**
 * 指标劣化独立页（T8）：展示扫描概况、当前劣化告警与最近恢复记录。
 * 数据源与告警中心同一张 alarm_event 表（Source=degradation）。
 */
export default function Degradation() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [scanMsg, setScanMsg] = useState<string>('');

  const { data, isFetching, refetch, error } = useQuery<DegradationOverview>({
    queryKey: ['degradation-overview'],
    queryFn: () => api.alarmDegradation(),
    refetchInterval: 30000,
  });

  const scan = useMutation({
    mutationFn: () => api.alarmDegradationScan(),
    onSuccess: (res) => {
      setScanMsg(
        `扫描 ${res.scanned} 个目标，命中 ${res.degraded.length} 项，本轮新建 ${res.created} 条、自动排障 ${res.autoDiag} 个${
          res.error ? `；异常：${res.error}` : ''
        }`,
      );
      message.success('指标劣化扫描完成');
      qc.invalidateQueries({ queryKey: ['degradation-overview'] });
      qc.invalidateQueries({ queryKey: ['alarms'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const firingColumns = useMemo(
    () => [
      {
        title: '主机 / 实例',
        dataIndex: 'host',
        width: 260,
        render: (_: string, r: AlarmEvent) => (
          <Space direction="vertical" size={0}>
            <Typography.Text strong style={{ fontSize: 12 }}>
              {r.host || r.instance}
            </Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              {r.instance}
            </Typography.Text>
          </Space>
        ),
      },
      {
        title: '指标 / 告警',
        dataIndex: 'alertName',
        width: 180,
        render: (v: string) => <Tag color="gold">{v}</Tag>,
      },
      {
        title: '等级',
        dataIndex: 'severity',
        width: 80,
        render: (v: string) => <Tag color={SEVERITY_COLOR[v] ?? 'default'}>{SEVERITY_LABEL[v] ?? v}</Tag>,
      },
      {
        title: '劣化说明',
        dataIndex: 'summary',
        ellipsis: true,
        render: (v: string) => <Typography.Text style={{ fontSize: 12 }}>{v || '-'}</Typography.Text>,
      },
      {
        title: '首次发现',
        dataIndex: 'firstSeenAt',
        width: 160,
        render: (v: string) => <span style={{ fontSize: 12 }}>{fmtTime(v)}</span>,
      },
      {
        title: '处置',
        width: 150,
        render: (_: unknown, r: AlarmEvent) => (
          <Space size={4}>
            {r.diagSessionId ? (
              <Tag color="blue" style={{ cursor: 'pointer' }} onClick={() => navigate('/diagnosis')}>
                已开排障
              </Tag>
            ) : (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                未开排障
              </Typography.Text>
            )}
            {r.ticketId && <Tag color="purple">已建单</Tag>}
          </Space>
        ),
      },
    ],
    [navigate],
  );

  const resolvedColumns = useMemo(
    () => [
      { title: '主机', dataIndex: 'host', width: 240, ellipsis: true },
      { title: '告警', dataIndex: 'alertName', width: 180 },
      {
        title: '恢复时间',
        dataIndex: 'resolvedAt',
        width: 170,
        render: (v: string, r: AlarmEvent) => fmtTime(v || r.lastSeenAt),
      },
      { title: '累计触发', dataIndex: 'fireCount', width: 90 },
    ],
    [],
  );

  const last = data?.lastScan;

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <LineChartOutlined style={{ color: '#C77700' }} />
          <Typography.Text strong>指标劣化</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            基于 Prometheus 时序的趋势扫描：未超阈值但持续爬升即触发 warning，与告警中心共用去重 / 恢复 / 自动排障
          </Typography.Text>
          <div style={{ flex: 1 }} />
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            loading={scan.isPending}
            onClick={() => scan.mutate()}
          >
            立即扫描
          </Button>
          <Button icon={<ReloadOutlined />} loading={isFetching} onClick={() => refetch()}>
            刷新
          </Button>
        </Space>
      </Card>

      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={(error as Error).message} />
      )}
      {scanMsg && (
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message={scanMsg} closable />
      )}

      <Row gutter={[12, 12]}>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="最近扫描目标"
              value={last?.scanned ?? 0}
              prefix={<LineChartOutlined />}
              valueStyle={{ color: '#1D4ED8' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {last ? `扫描时间 ${fmtTime(last.at)}` : '尚未扫描'}
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="当前劣化指标"
              value={last?.degraded.length ?? 0}
              prefix={<AlertOutlined />}
              valueStyle={{ color: '#C41E1A' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              窗口 {last?.windowMin ?? 0} 分钟 · 阈值 {last?.thresholdPct ?? 0}%
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic title="受影响主机" value={data?.affectedHosts ?? 0} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              按 host 去重后的劣化对象数
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="本轮自动排障"
              value={last?.autoDiag ?? 0}
              valueStyle={{ color: '#248A3D' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {last?.error ? `扫描异常：${last.error}` : '通过准入后自动创建会话'}
            </Typography.Text>
          </Card>
        </Col>
      </Row>

      <Card
        size="small"
        title={`劣化中（${data?.firing.length ?? 0}）`}
        style={{ marginTop: 12 }}
        extra={
          <Button size="small" type="link" onClick={() => navigate('/alert-center')}>
            去告警中心
          </Button>
        }
      >
        <Table
          rowKey="fingerprint"
          size="small"
          loading={!data && isFetching}
          columns={firingColumns}
          dataSource={data?.firing ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty description="当前没有劣化项" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
        />
      </Card>

      <Card size="small" title="最近恢复" style={{ marginTop: 12 }}>
        <Table
          rowKey="fingerprint"
          size="small"
          loading={!data && isFetching}
          columns={resolvedColumns}
          dataSource={data?.resolved ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty description="暂无恢复记录" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
        />
      </Card>

      {last && (
        <Card size="small" title="最近一轮扫描详情" style={{ marginTop: 12 }}>
          <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 4 }}>
            <Descriptions.Item label="扫描目标">{last.scanned}</Descriptions.Item>
            <Descriptions.Item label="命中劣化">{last.degraded.length}</Descriptions.Item>
            <Descriptions.Item label="新建 / 恢复">{`${last.created} / ${last.resolved}`}</Descriptions.Item>
            <Descriptions.Item label="自动排障">{last.autoDiag}</Descriptions.Item>
            <Descriptions.Item label="时间窗口">{last.windowMin} 分钟</Descriptions.Item>
            <Descriptions.Item label="劣化阈值">{last.thresholdPct}%</Descriptions.Item>
            <Descriptions.Item label="扫描时间">{fmtTime(last.at)}</Descriptions.Item>
            <Descriptions.Item label="异常">{last.error || '无'}</Descriptions.Item>
          </Descriptions>
        </Card>
      )}
    </>
  );
}
