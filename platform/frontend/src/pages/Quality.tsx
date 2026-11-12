import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  List,
  Progress,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import { LineChartOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { api } from '../api';
import type { BizMetric, QualityMetric, QualityOverview, QualityReport } from '../types';

const LEVEL_META: Record<QualityReport['level'], { color: string; text: string; stroke: string }> = {
  normal: { color: 'green', text: '正常', stroke: '#248A3D' },
  suspected: { color: 'orange', text: '疑似质差', stroke: '#C77700' },
  degraded: { color: 'red', text: '质差', stroke: '#C41E1A' },
};

const ROOT_CAUSE_LABEL: Record<string, string> = {
  database: '数据库',
  cache: '缓存',
  mq: '消息队列',
  gateway: '网关',
  application: '应用',
};

interface MetricMeta {
  label: string;
  unit: string;
  scale: number;
  /** 指标增长是否为劣化 */
  badOnUp: boolean;
  hint: string;
}

const METRIC_META: Record<BizMetric, MetricMeta> = {
  error_rate: {
    label: '错误率',
    unit: '%',
    scale: 100,
    badOnUp: true,
    hint: '上升为劣化，下降为改善',
  },
  tp99: {
    label: 'TP99',
    unit: '秒',
    scale: 1,
    badOnUp: true,
    hint: '上升为劣化，下降为改善',
  },
  request_total: {
    label: '请求量',
    unit: 'req/s',
    scale: 1,
    badOnUp: false,
    hint: '下跌为劣化，上升为改善',
  },
};

const METRIC_ORDER: BizMetric[] = ['error_rate', 'tp99', 'request_total'];

const WINDOW_OPTIONS = [
  { value: 30, label: '30 分钟' },
  { value: 60, label: '60 分钟' },
  { value: 180, label: '180 分钟' },
];

function fmtTime(v?: string): string {
  if (!v) return '-';
  return v.replace('T', ' ').slice(0, 19);
}

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

function fmtMetric(v: number, metric: BizMetric): string {
  const meta = METRIC_META[metric] ?? METRIC_META.error_rate;
  return `${(v * meta.scale).toFixed(4)} ${meta.unit}`;
}

function growthColor(metric: BizMetric, growthPct: number): string {
  if (Math.abs(growthPct) < 0.01) return '#8c8c8c';
  const up = growthPct > 0;
  const bad = METRIC_META[metric]?.badOnUp ? up : !up;
  return bad ? '#C41E1A' : '#248A3D';
}

/** 简单的基线 / 近期对比条，纯 CSS 无图表依赖。 */
function TrendBars({ baseline, recent, color }: { baseline: number; recent: number; color: string }) {
  const max = Math.max(baseline, recent, 1e-9);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 3, marginTop: 6 }}>
      {[
        { label: '基线', value: baseline, bar: '#BFBFBF' },
        { label: '近期', value: recent, bar: color },
      ].map((row) => (
        <div key={row.label} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ width: 28, fontSize: 11, color: '#8c8c8c' }}>{row.label}</span>
          <div style={{ flex: 1, height: 8, background: '#F0F0F0', borderRadius: 4 }}>
            <div
              style={{
                width: `${(row.value / max) * 100}%`,
                height: '100%',
                background: row.bar,
                borderRadius: 4,
                minWidth: row.value > 0 ? 2 : 0,
              }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

function MetricCard({ m }: { m: QualityMetric }) {
  const meta = METRIC_META[m.name] ?? METRIC_META.error_rate;
  const color = growthColor(m.name, m.growthPct);
  return (
    <Card size="small" title={meta.label}>
      <Space direction="vertical" size={2} style={{ width: '100%' }}>
        <Typography.Text style={{ fontSize: 12 }}>
          基线 {fmtMetric(m.baseline, m.name)} → 近期{' '}
          <Typography.Text strong style={{ color }}>
            {fmtMetric(m.recent, m.name)}
          </Typography.Text>
        </Typography.Text>
        <Space size={8}>
          <Typography.Text strong style={{ color, fontSize: 18 }}>
            {m.growthPct > 0 ? '+' : ''}
            {m.growthPct.toFixed(1)}%
          </Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {m.points} 个数据点
          </Typography.Text>
        </Space>
        <TrendBars baseline={m.baseline} recent={m.recent} color={color} />
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          {meta.hint}
        </Typography.Text>
      </Space>
    </Card>
  );
}

/**
 * 质差分析：按应用 + 时间窗口比对指标基线，输出质差评分、根因候选与证据链。
 * 质差降级会写入告警中心（Source=quality）并自动排障。
 */
export default function Quality() {
  const qc = useQueryClient();
  const { message } = AntdApp.useApp();
  const [appId, setAppId] = useState<string | undefined>();
  const [windowMin, setWindowMin] = useState(60);

  const apps = useQuery({ queryKey: ['apps-quality'], queryFn: () => api.topoApps() });

  const { data, isFetching, refetch, error } = useQuery<QualityOverview>({
    queryKey: ['quality-overview', appId ?? ''],
    queryFn: () => api.qualityOverview(appId),
  });

  const scan = useMutation({
    mutationFn: () => api.qualityScan(appId ?? '', windowMin),
    onSuccess: (r) => {
      message.success(
        `分析完成：${LEVEL_META[r.level]?.text ?? r.level}，评分 ${(r.score * 100).toFixed(1)} 分`,
      );
      qc.invalidateQueries({ queryKey: ['quality-overview'] });
      qc.invalidateQueries({ queryKey: ['alarms'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  // 未选应用时回落到最近一次报告（history 已按时间倒序）。
  const report: QualityReport | undefined = data?.latest ?? data?.history?.[0];
  const level = report ? LEVEL_META[report.level] : undefined;

  const historyColumns = [
    {
      title: '分析时间',
      dataIndex: 'at',
      width: 160,
      render: (v: string) => <span style={{ fontSize: 12 }}>{fmtTime(v)}</span>,
    },
    { title: '应用', dataIndex: 'appId', width: 150, ellipsis: true },
    {
      title: '等级',
      dataIndex: 'level',
      width: 110,
      render: (v: QualityReport['level']) => (
        <Tag color={LEVEL_META[v]?.color}>{LEVEL_META[v]?.text ?? v}</Tag>
      ),
    },
    {
      title: '评分',
      dataIndex: 'score',
      width: 90,
      render: (v: number) => `${(v * 100).toFixed(1)}%`,
    },
    {
      title: '窗口',
      dataIndex: 'windowMin',
      width: 80,
      render: (v: number) => `${v} 分钟`,
    },
    {
      title: '根因',
      width: 200,
      ellipsis: true,
      render: (_: unknown, r: QualityReport) =>
        r.rootCause ? (
          <span style={{ fontSize: 12 }}>
            {ROOT_CAUSE_LABEL[r.rootCause.type] ?? r.rootCause.type} · {r.rootCause.target}
          </span>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            证据不足
          </Typography.Text>
        ),
    },
    {
      title: '关联告警',
      dataIndex: 'alarmFingerprint',
      width: 110,
      render: (v: string | undefined, r: QualityReport) =>
        v ? <Link to={`/alert-center?q=${encodeURIComponent(r.appId)}`}>查看告警</Link> : '-',
    },
  ];

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <LineChartOutlined style={{ color: '#C77700' }} />
          <Typography.Text strong>质差分析</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            按窗口比对错误率 / TP99 / 请求量基线，输出质差等级、根因候选与证据链
          </Typography.Text>
          <div style={{ flex: 1 }} />
          <Select
            style={{ width: 260 }}
            showSearch
            allowClear
            optionFilterProp="label"
            placeholder="选择应用（不选则看最近报告）"
            value={appId}
            loading={apps.isFetching}
            onChange={(v?: string) => setAppId(v)}
            options={(apps.data?.apps ?? []).map((a) => ({
              value: a.appId,
              label: a.appName ? `${a.appName}（${a.appId}）` : a.appId,
            }))}
          />
          <Select
            style={{ width: 120 }}
            value={windowMin}
            onChange={setWindowMin}
            options={WINDOW_OPTIONS}
          />
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            loading={scan.isPending}
            disabled={!appId}
            onClick={() => scan.mutate()}
          >
            开始分析
          </Button>
          <Button icon={<ReloadOutlined />} loading={isFetching} onClick={() => refetch()}>
            刷新
          </Button>
        </Space>
      </Card>

      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={(error as Error).message} />
      )}
      {!appId && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="未选择应用，当前展示最近一次质差报告；选择应用后可执行新的分析。"
        />
      )}

      {!report ? (
        <Card size="small">
          <Empty
            description={
              appId ? '该应用暂无质差报告，点击「开始分析」生成' : '暂无质差报告，请先选择应用并分析'
            }
          />
        </Card>
      ) : (
        <>
          <Row gutter={[12, 12]}>
            <Col xs={24} lg={12}>
              <Card
                size="small"
                title="分析结论"
                extra={
                  data?.firing.length ? (
                    <Link to={`/alert-center?q=${encodeURIComponent(report.appId)}`}>
                      当前质差告警 {data.firing.length} 条
                    </Link>
                  ) : null
                }
              >
                <Space direction="vertical" size={10} style={{ width: '100%' }}>
                  <Space size={8}>
                    <Tag color={level?.color}>{level?.text ?? report.level}</Tag>
                    <Typography.Text strong style={{ fontSize: 14 }}>
                      {report.appId}
                    </Typography.Text>
                  </Space>
                  <div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      质差评分
                    </Typography.Text>
                    <Progress
                      percent={Math.min(100, Math.max(0, report.score * 100))}
                      strokeColor={level?.stroke}
                      format={() => pct(report.score)}
                    />
                  </div>
                  <Descriptions size="small" column={2}>
                    <Descriptions.Item label="分析时间">{fmtTime(report.at)}</Descriptions.Item>
                    <Descriptions.Item label="窗口">{report.windowMin} 分钟</Descriptions.Item>
                    <Descriptions.Item label="报告 ID" span={2}>
                      <Typography.Text copyable style={{ fontSize: 12 }}>
                        {report.id}
                      </Typography.Text>
                    </Descriptions.Item>
                  </Descriptions>
                </Space>
              </Card>
            </Col>
            {METRIC_ORDER.map((name) => {
              const m = report.metrics.find((x) => x.name === name);
              return (
                <Col xs={24} sm={8} lg={4} key={name}>
                  {m ? (
                    <MetricCard m={m} />
                  ) : (
                    <Card size="small" title={METRIC_META[name].label}>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        数据点不足，未参与评分
                      </Typography.Text>
                    </Card>
                  )}
                </Col>
              );
            })}
          </Row>

          <Row gutter={[12, 12]} style={{ marginTop: 12 }}>
            <Col xs={24} lg={12}>
              <Card size="small" title="根因候选">
                {report.rootCause ? (
                  <Descriptions size="small" column={1}>
                    <Descriptions.Item label="类型">
                      <Tag color="purple">
                        {ROOT_CAUSE_LABEL[report.rootCause.type] ?? report.rootCause.type}
                      </Tag>
                    </Descriptions.Item>
                    <Descriptions.Item label="对象">{report.rootCause.target || '-'}</Descriptions.Item>
                    <Descriptions.Item label="地址">{report.rootCause.addr || '-'}</Descriptions.Item>
                    <Descriptions.Item label="置信度">
                      {report.rootCause.score.toFixed(2)}
                    </Descriptions.Item>
                    <Descriptions.Item label="判断依据">
                      <Typography.Text style={{ fontSize: 12 }}>
                        {report.rootCause.reason || '-'}
                      </Typography.Text>
                    </Descriptions.Item>
                  </Descriptions>
                ) : (
                  <Empty description="证据不足，暂未定位根因" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                )}
              </Card>
            </Col>
            <Col xs={24} lg={12}>
              <Card size="small" title={`证据链（${report.evidence.length}）`}>
                <List
                  size="small"
                  dataSource={report.evidence}
                  renderItem={(t) => (
                    <List.Item style={{ paddingLeft: 0 }}>
                      <Typography.Text style={{ fontSize: 12 }}>{t}</Typography.Text>
                    </List.Item>
                  )}
                  locale={{ emptyText: '暂无证据' }}
                />
              </Card>
            </Col>
          </Row>
        </>
      )}

      <Card size="small" title={`历史报告（${data?.history.length ?? 0}）`} style={{ marginTop: 12 }}>
        <Table
          rowKey="id"
          size="small"
          loading={!data && isFetching}
          columns={historyColumns}
          dataSource={data?.history ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{
            emptyText: (
              <Empty description="暂无历史报告" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            ),
          }}
        />
      </Card>
    </>
  );
}
