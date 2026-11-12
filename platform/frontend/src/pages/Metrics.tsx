import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Progress,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import {
  ClockCircleOutlined,
  ReloadOutlined,
  RobotOutlined,
  ThunderboltOutlined,
  TrophyOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { KpiDailyPoint, KpiLatency, KpiResult } from '../types';

const RANGE_OPTIONS = [
  { value: 7, label: '近 7 天' },
  { value: 30, label: '近 30 天' },
  { value: 90, label: '近 90 天' },
];

/** 秒 → 人类可读（45.2s / 3m20s）。 */
function fmtSec(v: number): string {
  if (!v) return '0s';
  if (v < 60) return `${v.toFixed(1)}s`;
  const m = Math.floor(v / 60);
  const s = Math.round(v % 60);
  return s ? `${m}m${s}s` : `${m}m`;
}

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

interface RowDef {
  key: string;
  name: string;
  target: string;
  source: KpiLatency;
}

const LATENCY_COLUMNS = [
  { title: '环节', dataIndex: 'name', width: 200 },
  { title: '考核目标', dataIndex: 'target', width: 120 },
  {
    title: '样本',
    width: 80,
    render: (_: unknown, r: RowDef) =>
      r.source.samples ? r.source.samples : <Typography.Text type="secondary">0</Typography.Text>,
  },
  { title: 'P50', render: (_: unknown, r: RowDef) => fmtSec(r.source.p50) },
  { title: 'P90', render: (_: unknown, r: RowDef) => fmtSec(r.source.p90) },
  { title: '均值', render: (_: unknown, r: RowDef) => fmtSec(r.source.avg) },
  {
    title: '达标率',
    width: 150,
    render: (_: unknown, r: RowDef) =>
      r.source.samples === 0 ? (
        <Typography.Text type="secondary">无样本</Typography.Text>
      ) : (
        <Space size={6}>
          <Tag color={r.source.rate >= 0.9 ? 'green' : r.source.rate >= 0.7 ? 'orange' : 'red'}>
            {pct(r.source.rate)}
          </Tag>
          <Progress
            percent={Math.round(r.source.rate * 100)}
            size="small"
            showInfo={false}
            style={{ width: 60 }}
          />
        </Space>
      ),
  },
];

/** 单日柱状趋势：告警 / 排障 / 工单 / 巡检批次。 */
function DailyChart({ points }: { points: KpiDailyPoint[] }) {
  if (!points.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />;
  const max = Math.max(1, ...points.map((p) => Math.max(p.alarms, p.sessions, p.tickets, p.runs)));
  const bar = (v: number, color: string) => (
    <div
      style={{
        flex: 1,
        height: `${(v / max) * 100}%`,
        minHeight: v ? 2 : 0,
        background: color,
        borderRadius: 1,
      }}
    />
  );
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: 6, height: 120 }}>
        {points.map((p) => (
          <div
            key={p.date}
            style={{ flex: 1, display: 'flex', alignItems: 'flex-end', gap: 2, height: '100%' }}
            title={`${p.date} 告警 ${p.alarms} / 排障 ${p.sessions} / 工单 ${p.tickets} / 巡检 ${p.runs}`}
          >
            {bar(p.alarms, '#C41E1A')}
            {bar(p.sessions, '#1D4ED8')}
            {bar(p.tickets, '#C77700')}
            {bar(p.runs, '#248A3D')}
          </div>
        ))}
      </div>
      <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
        {points.map((p, i) => (
          <div key={p.date} style={{ flex: 1, textAlign: 'center', fontSize: 10, color: '#86868B' }}>
            {i % Math.ceil(points.length / 8) === 0 ? p.date.slice(5) : ''}
          </div>
        ))}
      </div>
      <Space size={16} style={{ marginTop: 6 }} wrap>
        {[
          ['告警', '#C41E1A'],
          ['排障', '#1D4ED8'],
          ['工单', '#C77700'],
          ['巡检', '#248A3D'],
        ].map(([label, color]) => (
          <Space size={4} key={label}>
            <span style={{ width: 8, height: 8, background: color, borderRadius: 2, display: 'inline-block' }} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {label}
            </Typography.Text>
          </Space>
        ))}
      </Space>
    </div>
  );
}

export default function Metrics() {
  const [days, setDays] = useState(7);
  const { data, isFetching, refetch, error } = useQuery<KpiResult>({
    queryKey: ['kpi', days],
    queryFn: () => api.metricsKpi(days),
  });

  const rows: RowDef[] = data
    ? [
        { key: 'a2s', name: '告警 → 排障启动', target: '≤ 1 分钟', source: data.alertToSession },
        { key: 's2r', name: '排障耗时（定位）', target: '≤ 5 分钟', source: data.sessionToRootCause },
        { key: 'r2t', name: '定位 → 建单', target: '≤ 5 分钟', source: data.rootCauseToTicket },
        { key: 'a2t', name: '告警 → 建单（闭环）', target: '≤ 10 分钟', source: data.alertToTicket },
      ]
    : [];

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <TrophyOutlined style={{ color: '#C77700' }} />
          <Typography.Text strong>考核指标看板</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            口径：告警首次入库 → 排障启动 → 定位完成 → 自动建单
          </Typography.Text>
          <div style={{ flex: 1 }} />
          <Select
            value={days}
            style={{ width: 110 }}
            options={RANGE_OPTIONS}
            onChange={(v) => setDays(v)}
          />
          <Button icon={<ReloadOutlined />} loading={isFetching} onClick={() => refetch()}>
            刷新
          </Button>
        </Space>
      </Card>

      {error && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message={(error as Error).message}
        />
      )}

      <Row gutter={[12, 12]}>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small" loading={!data && isFetching}>
            <Statistic
              title="1 分钟发现达标率"
              value={data ? data.alertToSession.rate * 100 : 0}
              precision={1}
              suffix="%"
              prefix={<ThunderboltOutlined />}
              valueStyle={{ color: '#C41E1A' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              P90 {fmtSec(data?.alertToSession.p90 ?? 0)} · {data?.alertToSession.samples ?? 0} 个样本
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small" loading={!data && isFetching}>
            <Statistic
              title="5 分钟定位达标率"
              value={data ? data.sessionToRootCause.rate * 100 : 0}
              precision={1}
              suffix="%"
              prefix={<ClockCircleOutlined />}
              valueStyle={{ color: '#1D4ED8' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              P90 {fmtSec(data?.sessionToRootCause.p90 ?? 0)} · {data?.sessionToRootCause.samples ?? 0} 个样本
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small" loading={!data && isFetching}>
            <Statistic
              title="10 分钟闭环达标率"
              value={data ? data.alertToTicket.rate * 100 : 0}
              precision={1}
              suffix="%"
              prefix={<TrophyOutlined />}
              valueStyle={{ color: '#248A3D' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              P90 {fmtSec(data?.alertToTicket.p90 ?? 0)} · {data?.alertToTicket.samples ?? 0} 个样本
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small" loading={!data && isFetching}>
            <Statistic
              title="巡检自动化执行率"
              value={data ? data.automation.inspectCronRate * 100 : 0}
              precision={1}
              suffix="%"
              prefix={<RobotOutlined />}
              valueStyle={{ color: '#C77700' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              cron 批次 {data?.counts.runs ?? 0} 个 · 目标 ≥ 95%
            </Typography.Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={[12, 12]} style={{ marginTop: 12 }}>
        <Col xs={24} lg={14}>
          <Card size="small" title="时延分布与达标率">
            <Table
              rowKey="key"
              size="small"
              pagination={false}
              columns={LATENCY_COLUMNS}
              dataSource={rows}
              loading={!data && isFetching}
              locale={{ emptyText: '统计区间内暂无排障样本' }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card size="small" title="自动化率">
            <Space direction="vertical" size={10} style={{ width: '100%' }}>
              {[
                { label: '告警自动开排障', v: data?.automation.autoDiagRate ?? 0, hint: `${data?.counts.withDiag ?? 0}/${data?.counts.alarms ?? 0} 条告警` },
                { label: '自动建单', v: data?.automation.autoTicketRate ?? 0, hint: `${data?.counts.withTicket ?? 0}/${data?.counts.withDiag ?? 0} 个会话` },
                { label: '定位成功率', v: data?.automation.diagSuccessRate ?? 0, hint: `${data?.counts.sessions ?? 0} 个会话` },
                { label: '巡检自动化', v: data?.automation.inspectCronRate ?? 0, hint: `${data?.counts.runs ?? 0} 个批次` },
                {
                  label: '根因准确率（人工标注）',
                  v: data?.accuracy.rate ?? 0,
                  hint: `已标注 ${data?.accuracy.labeled ?? 0} 个 · 覆盖率 ${pct(data?.accuracy.coverage ?? 0)}`,
                },
              ].map((it) => (
                <div key={it.label}>
                  <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                    <Typography.Text>{it.label}</Typography.Text>
                    <Typography.Text strong>{pct(it.v)}</Typography.Text>
                  </Space>
                  <Progress
                    percent={Math.round(it.v * 100)}
                    size="small"
                    showInfo={false}
                    strokeColor={it.v >= 0.95 ? '#248A3D' : it.v >= 0.7 ? '#C77700' : '#C41E1A'}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {it.hint}
                  </Typography.Text>
                </div>
              ))}
            </Space>
          </Card>
        </Col>
      </Row>

      <Card size="small" title="每日趋势" style={{ marginTop: 12 }}>
        {data ? <DailyChart points={data.daily} /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />}
      </Card>

      {data && (
        <Alert
          type="info"
          showIcon
          style={{ marginTop: 12 }}
          message="统计口径说明"
          description={
            <Space direction="vertical" size={2}>
              <Typography.Text style={{ fontSize: 12 }}>{data.note}</Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                区间 {data.startAt.slice(0, 10)} ~ {data.endAt.slice(0, 10)}；告警 {data.counts.alarms} 条（
                {Object.entries(data.bySource)
                  .map(([k, v]) => `${k === 'degradation' ? '指标劣化' : 'Prometheus'} ${v}`)
                  .join(' / ')}
                ）；排障 {data.counts.sessions} 个、工单 {data.counts.tickets} 张。
              </Typography.Text>
            </Space>
          }
        />
      )}
    </>
  );
}
