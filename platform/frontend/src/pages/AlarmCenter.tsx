import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Descriptions,
  Drawer,
  Dropdown,
  Empty,
  Input,
  Select,
  Space,
  Statistic,
  Row,
  Col,
  Table,
  Tag,
  Timeline,
  Tooltip,
  Typography,
} from 'antd';
import {
  AlertOutlined,
  BellOutlined,
  BugOutlined,
  ClockCircleOutlined,
  ReloadOutlined,
  RobotOutlined,
  SendOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type {
  AlarmEvent,
  AlarmSeverity,
  AlarmSilenceDuration,
  AlarmState,
  AlarmTrendPoint,
  NotificationLog,
} from '../types';

const SEVERITY_TONE: Record<AlarmSeverity, PillTone> = {
  critical: 'risk',
  warning: 'warn',
  info: 'info',
  other: 'neutral',
};

const SEVERITY_LABEL: Record<AlarmSeverity, string> = {
  critical: '严重',
  warning: '警告',
  info: '提示',
  other: '其他',
};

const STATE_TONE: Record<AlarmState, PillTone> = {
  firing: 'risk',
  resolved: 'ok',
};

// 告警来源：prometheus 走 list_alerts 采集，degradation 指标劣化扫描生成。
const SOURCE_LABEL: Record<string, string> = {
  prometheus: 'Prometheus',
  degradation: '指标劣化',
  alertmanager: 'Alertmanager',
  inspection: '巡检',
};

const NOTIFY_STATUS_TAG: Record<NotificationLog['status'], { color: string; text: string }> = {
  sent: { color: 'green', text: '已送达' },
  failed: { color: 'red', text: '发送失败' },
  skipped: { color: 'default', text: '未配置' },
};

const STATE_LABEL: Record<AlarmState, string> = {
  firing: '告警中',
  resolved: '已恢复',
};

const DIAG_TONE: Record<string, PillTone> = {
  root_caused: 'ok',
  investigating: 'info',
  open: 'neutral',
  failed: 'risk',
};

const DIAG_LABEL: Record<string, string> = {
  root_caused: '根因已定位',
  investigating: '排障中',
  open: '未处理',
  failed: '排障失败',
};

const SILENCE_MENU: { key: AlarmSilenceDuration; label: string }[] = [
  { key: '30m', label: '静默 30 分钟' },
  { key: '2h', label: '静默 2 小时' },
  { key: '24h', label: '静默 24 小时' },
  { key: 'forever', label: '永久静默' },
];

function isSilenced(a: AlarmEvent): boolean {
  return !!a.silencedUntil && new Date(a.silencedUntil).getTime() > Date.now();
}

/** 24h 告警趋势柱图（红=新增 / 绿=恢复，纯 CSS 无额外依赖） */
function TrendChart({ points }: { points: AlarmTrendPoint[] }) {
  const max = Math.max(1, ...points.map((p) => Math.max(p.created, p.resolved)));
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: 2, height: 110 }}>
        {points.map((p) => (
          <div
            key={p.hour}
            title={`${p.hour.replace('T', ' ')}:00　新增 ${p.created} / 恢复 ${p.resolved}`}
            style={{ flex: 1, display: 'flex', alignItems: 'flex-end', gap: 1, height: '100%' }}
          >
            <div
              style={{
                flex: 1,
                height: `${(p.created / max) * 100}%`,
                minHeight: p.created ? 2 : 0,
                background: '#C41E1A',
                borderRadius: 1,
              }}
            />
            <div
              style={{
                flex: 1,
                height: `${(p.resolved / max) * 100}%`,
                minHeight: p.resolved ? 2 : 0,
                background: '#248A3D',
                borderRadius: 1,
              }}
            />
          </div>
        ))}
      </div>
      <div style={{ display: 'flex', gap: 2, marginTop: 4 }}>
        {points.map((p, i) => (
          <div key={p.hour} style={{ flex: 1, textAlign: 'center', fontSize: 10, color: '#86868B' }}>
            {i % 4 === 0 ? p.hour.slice(11, 13) : ''}
          </div>
        ))}
      </div>
      <Space size={16} style={{ marginTop: 6 }}>
        <Space size={4}>
          <span style={{ width: 8, height: 8, background: '#C41E1A', borderRadius: 2, display: 'inline-block' }} />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            新增
          </Typography.Text>
        </Space>
        <Space size={4}>
          <span style={{ width: 8, height: 8, background: '#248A3D', borderRadius: 2, display: 'inline-block' }} />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            恢复
          </Typography.Text>
        </Space>
      </Space>
    </div>
  );
}

export default function AlarmCenter() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const navigate = useNavigate();

  // 来自拓扑 / 外部链接的初始筛选（host 优先于 q / keyword）
  const [searchParams, setSearchParams] = useSearchParams();
  const urlHost = (searchParams.get('host') ?? '').trim();
  const urlKeyword = (searchParams.get('q') ?? searchParams.get('keyword') ?? '').trim();
  const initialKeyword = urlHost || urlKeyword;
  const [urlFilter, setUrlFilter] = useState<'host' | 'keyword' | null>(
    urlHost ? 'host' : urlKeyword ? 'keyword' : null,
  );

  const [stateFilter, setStateFilter] = useState<AlarmState | undefined>();
  const [severityFilter, setSeverityFilter] = useState<AlarmSeverity | undefined>();
  const [keyword, setKeyword] = useState(initialKeyword);
  const [searchText, setSearchText] = useState(initialKeyword);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [viewing, setViewing] = useState<AlarmEvent | null>(null);
  const [notifyOpen, setNotifyOpen] = useState(false);

  const resetPage = () => setPage(1);

  // 关闭「来自拓扑/链接」提示：清空关键词与 URL 参数，不影响其他筛选
  const clearUrlFilter = () => {
    setUrlFilter(null);
    setKeyword('');
    setSearchText('');
    setPage(1);
    setSearchParams({});
  };

  // 告警所属主机：优先 host，其次 instance 的主机部分
  const hostOf = (a: AlarmEvent) => a.host || (a.instance ? a.instance.split(':')[0] : '');

  // —— 查询 ——
  const alarms = useQuery({
    queryKey: ['alarms', stateFilter, severityFilter, keyword, page, pageSize],
    queryFn: () =>
      api.alarmList({
        state: stateFilter,
        severity: severityFilter,
        q: keyword || undefined,
        page,
        pageSize,
      }),
    refetchInterval: 15000, // 每 15 秒自动刷新
  });

  const stats = useQuery({
    queryKey: ['alarms-stats'],
    queryFn: api.alarmStats,
    refetchInterval: 15000,
  });

  const trend = useQuery({
    queryKey: ['alarms-trend'],
    queryFn: () => api.alarmTrend(24),
    refetchInterval: 60000,
  });

  // —— 通知出站记录（T11）——
  const notifications = useQuery({
    queryKey: ['alarm-notifications'],
    queryFn: () => api.alarmNotifications(50),
    enabled: notifyOpen,
  });

  const notifyTest = useMutation({
    mutationFn: () => api.alarmNotifyTest(),
    onSuccess: (res) => {
      if (res.status === 'sent') {
        message.success(`测试通知已发送（HTTP ${res.httpStatus ?? 200}）`);
      } else if (res.status === 'skipped') {
        message.warning('未配置 ALARM_NOTIFY_URL，通知出站当前关闭');
      } else {
        message.error(`测试通知发送失败：${res.error ?? '未知错误'}`);
      }
      qc.invalidateQueries({ queryKey: ['alarm-notifications'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  // —— 手动触发轮询 ——
  const poll = useMutation({
    mutationFn: () => api.alarmPoll(),
    onSuccess: (resp) => {
      qc.invalidateQueries({ queryKey: ['alarms'] });
      qc.invalidateQueries({ queryKey: ['alarms-stats'] });
      qc.invalidateQueries({ queryKey: ['alarms-trend'] });
      const parts: string[] = [];
      if (resp.upserted > 0) parts.push(`入库 ${resp.upserted} 条`);
      if (resp.autoDiag > 0) parts.push(`自动排障 ${resp.autoDiag} 条`);
      message.success(parts.length ? parts.join('，') : '轮询完成（无变化）');
    },
    onError: (e: Error) => message.error(e.message),
  });

  // —— 指标劣化扫描（S1）——
  const degradation = useMutation({
    mutationFn: () => api.alarmDegradationScan(),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['alarms'] });
      qc.invalidateQueries({ queryKey: ['alarms-stats'] });
      qc.invalidateQueries({ queryKey: ['alarms-trend'] });
      if (res.error) {
        message.warning(`扫描完成但有异常：${res.error}`);
        return;
      }
      const parts = [`扫描 ${res.scanned} 个目标`];
      if (res.degraded.length) parts.push(`劣化 ${res.degraded.length} 个`);
      if (res.created) parts.push(`新增告警 ${res.created} 条`);
      if (res.resolved) parts.push(`恢复 ${res.resolved} 条`);
      if (res.autoDiag) parts.push(`自动排障 ${res.autoDiag} 条`);
      message.success(parts.join('，'));
    },
    onError: (e: Error) => message.error(e.message),
  });

  // —— 强制开排障 ——
  const diagnose = useMutation({
    mutationFn: (fp: string) => api.alarmDiagnose(fp),
    onSuccess: (session) => {
      qc.invalidateQueries({ queryKey: ['alarms'] });
      message.success(`已创建排障会话 ${session.id.slice(0, 8)}…`);
    },
    onError: (e: Error) => message.error(e.message),
  });

  // —— 手动静默 ——
  const refreshAll = () => {
    qc.invalidateQueries({ queryKey: ['alarms'] });
    qc.invalidateQueries({ queryKey: ['alarms-stats'] });
  };
  const silence = useMutation({
    mutationFn: ({ fp, duration }: { fp: string; duration: AlarmSilenceDuration }) =>
      api.alarmSilence(fp, duration, '告警中心手动静默'),
    onSuccess: (_, v) => {
      const label = v.duration === 'forever' ? '永久' : v.duration;
      message.success(`已静默（${label}），静默期内不会自动开排障`);
      refreshAll();
    },
    onError: (e: Error) => message.error(e.message),
  });
  const unsilence = useMutation({
    mutationFn: (fp: string) => api.alarmUnsilence(fp),
    onSuccess: () => {
      message.success('已取消静默');
      refreshAll();
    },
    onError: (e: Error) => message.error(e.message),
  });

  const statsData = stats.data?.stats;
  const firingCritical = statsData?.critical.firing ?? 0;
  const firingWarning = statsData?.warning.firing ?? 0;
  const resolvedCount =
    (statsData?.critical.resolved ?? 0) +
    (statsData?.warning.resolved ?? 0) +
    (statsData?.info.resolved ?? 0) +
    (statsData?.other.resolved ?? 0);
  const rows = alarms.data?.items ?? [];
  const total = alarms.data?.total ?? 0;

  // 详情时间线
  const timelineItems = viewing
    ? [
        {
          color: 'red',
          children: (
            <Space direction="vertical" size={0}>
              <Typography.Text strong>告警产生</Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {new Date(viewing.firstSeenAt).toLocaleString()} · 已触发 {viewing.fireCount} 次
              </Typography.Text>
            </Space>
          ),
        },
        ...(viewing.sessionCreatedAt
          ? [
              {
                color: 'blue',
                children: (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>自动创建排障会话</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {new Date(viewing.sessionCreatedAt).toLocaleString()} · {viewing.diagSessionId}
                    </Typography.Text>
                  </Space>
                ),
              },
            ]
          : []),
        ...(viewing.ticketCreatedAt
          ? [
              {
                color: 'orange',
                children: (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>自动生成工单</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {new Date(viewing.ticketCreatedAt).toLocaleString()} · {viewing.ticketId}
                    </Typography.Text>
                  </Space>
                ),
              },
            ]
          : []),
        ...(viewing.resolvedAt
          ? [
              {
                color: 'green',
                children: (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>告警恢复</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {new Date(viewing.resolvedAt).toLocaleString()}
                    </Typography.Text>
                  </Space>
                ),
              },
            ]
          : [
              {
                color: 'gray',
                children: (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    仍在告警中（最近采集 {new Date(viewing.lastSeenAt).toLocaleString()}）
                  </Typography.Text>
                ),
              },
            ]),
      ]
    : [];

  return (
    <>
      {/* 顶部统计卡 */}
      <Row gutter={[12, 12]} style={{ marginBottom: 12 }}>
        <Col xs={24} sm={6}>
          <Card size="small">
            <Statistic
              title="严重告警"
              value={firingCritical}
              valueStyle={{ color: '#C41E1A' }}
              prefix={<ThunderboltOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card size="small">
            <Statistic
              title="警告"
              value={firingWarning}
              valueStyle={{ color: '#C77700' }}
              prefix={<AlertOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card size="small">
            <Statistic
              title="已恢复"
              value={resolvedCount}
              valueStyle={{ color: '#248A3D' }}
              prefix={<ReloadOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={6}>
          <Card size="small">
            <Statistic
              title="自动排障中"
              value={stats.data?.autoDiag ?? 0}
              valueStyle={{ color: '#0071E3' }}
              prefix={<RobotOutlined />}
            />
          </Card>
        </Col>
      </Row>

      {/* 24h 趋势 */}
      <Card
        size="small"
        title={
          <Space>
            <ClockCircleOutlined />
            <span>近 24h 告警趋势</span>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              按小时聚合
            </Typography.Text>
          </Space>
        }
        style={{ marginBottom: 12 }}
      >
        {trend.data?.points?.length ? (
          <TrendChart points={trend.data.points} />
        ) : (
          <Typography.Text type="secondary">暂无趋势数据</Typography.Text>
        )}
      </Card>

      {/* 来自拓扑 / 外部链接的筛选提示 */}
      {urlFilter && (
        <Alert
          type="info"
          showIcon
          closable
          style={{ marginBottom: 12 }}
          message={urlFilter === 'host' ? `来自拓扑：${urlHost}` : `来自链接：${urlKeyword}`}
          onClose={clearUrlFilter}
        />
      )}

      {/* 过滤 + 操作条 */}
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <Typography.Text type="secondary">状态:</Typography.Text>
          <Select
            placeholder="全部"
            style={{ width: 120 }}
            allowClear
            value={stateFilter}
            onChange={(v) => {
              setStateFilter(v);
              resetPage();
            }}
            options={[
              { value: 'firing', label: '告警中' },
              { value: 'resolved', label: '已恢复' },
            ]}
          />
          <Typography.Text type="secondary">等级:</Typography.Text>
          <Select
            placeholder="全部"
            style={{ width: 120 }}
            allowClear
            value={severityFilter}
            onChange={(v) => {
              setSeverityFilter(v);
              resetPage();
            }}
            options={(Object.keys(SEVERITY_LABEL) as AlarmSeverity[]).map((s) => ({
              value: s,
              label: SEVERITY_LABEL[s],
            }))}
          />
          <Input.Search
            placeholder="名称 / 实例 / 主机 / 现象"
            allowClear
            style={{ width: 260 }}
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            onSearch={(v) => {
              setKeyword(v.trim());
              setUrlFilter(null);
              resetPage();
            }}
          />
          <div style={{ flex: 1 }} />
          <Button icon={<BellOutlined />} onClick={() => setNotifyOpen(true)}>
            通知记录
          </Button>
          <Button
            icon={<ThunderboltOutlined />}
            loading={degradation.isPending}
            onClick={() => degradation.mutate()}
          >
            扫描劣化
          </Button>
          <Button icon={<ReloadOutlined />} loading={poll.isPending} onClick={() => poll.mutate()}>
            手动轮询
          </Button>
        </Space>
      </Card>

      {/* 告警列表 */}
      <Card
        size="small"
        title={
          <Space>
            <BugOutlined />
            <span>告警列表（{total}）</span>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              自动刷新 · 15s
            </Typography.Text>
          </Space>
        }
        extra={
          alarms.isRefetching ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              同步中…
            </Typography.Text>
          ) : null
        }
      >
        {alarms.isLoading && <div style={{ textAlign: 'center', padding: 40 }}>加载中…</div>}
        {!alarms.isLoading && rows.length === 0 && (
          <Empty
            description={
              <Space direction="vertical" size={4}>
                <span>暂无告警。</span>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  点右上角"手动轮询"触发 Prometheus 采集，或等 15s 自动刷新。
                </Typography.Text>
              </Space>
            }
          />
        )}
        {rows.length > 0 && (
          <Table
            size="small"
            rowKey="fingerprint"
            dataSource={rows}
            pagination={{
              current: page,
              pageSize,
              total,
              size: 'small',
              showSizeChanger: true,
              pageSizeOptions: ['10', '20', '50'],
              showTotal: (t) => `共 ${t} 条`,
              onChange: (p, ps) => {
                setPage(p);
                setPageSize(ps);
              },
            }}
            rowClassName={(a) => (a.state === 'firing' && a.severity === 'critical' ? 'row-critical' : '')}
            columns={[
              {
                title: '等级',
                width: 80,
                dataIndex: 'severity',
                render: (s: AlarmSeverity) => (
                  <StatusPill tone={SEVERITY_TONE[s]} size="sm" dot>
                    {SEVERITY_LABEL[s]}
                  </StatusPill>
                ),
              },
              {
                title: '状态',
                width: 150,
                render: (_, a) => (
                  <Space size={4} wrap>
                    <StatusPill tone={STATE_TONE[a.state]} size="sm" dot>
                      {STATE_LABEL[a.state]}
                    </StatusPill>
                    {isSilenced(a) && (
                      <Tag color="default" style={{ margin: 0 }}>
                        静默中
                      </Tag>
                    )}
                    {a.suppressedBy && (
                      <Tag color="purple" style={{ margin: 0 }}>
                        {a.suppressedBy === 'inhibited' ? '上游抑制' : '上游静默'}
                      </Tag>
                    )}
                  </Space>
                ),
              },
              {
                title: '告警名称 / 目标',
                ellipsis: true,
                render: (_, a) => (
                  <Space direction="vertical" size={0}>
                    <Space size={4}>
                      <Typography.Text strong>{a.alertName}</Typography.Text>
                      {a.source === 'degradation' && (
                        <Tag color="orange" style={{ margin: 0 }}>
                          指标劣化
                        </Tag>
                      )}
                    </Space>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {a.instance || a.host || a.labels?.node || '-'}
                    </Typography.Text>
                  </Space>
                ),
              },
              {
                title: '现象',
                ellipsis: true,
                dataIndex: 'summary',
                render: (v: string) => v || '-',
              },
              {
                title: '排障状态',
                width: 120,
                render: (_, a) => {
                  if (!a.diagSessionId) {
                    return <Typography.Text type="secondary">—</Typography.Text>;
                  }
                  const ds = a.diagStatus ?? 'open';
                  return (
                    <StatusPill tone={DIAG_TONE[ds] ?? 'neutral'} size="sm" dot>
                      {DIAG_LABEL[ds] ?? ds}
                      {a.confidence != null && a.confidence > 0 && ` · ${Math.round(a.confidence * 100)}%`}
                    </StatusPill>
                  );
                },
              },
              {
                title: '工单',
                width: 90,
                render: (_, a) => {
                  if (!a.ticketId) return <Typography.Text type="secondary">—</Typography.Text>;
                  return (
                    <Tag color="blue" style={{ cursor: 'pointer' }} onClick={() => navigate('/tickets')}>
                      已建单
                    </Tag>
                  );
                },
              },
              {
                title: '首次 / 最近',
                width: 200,
                render: (_, a) => (
                  <Space direction="vertical" size={0}>
                    <span style={{ fontSize: 12 }}>首见 {new Date(a.firstSeenAt).toLocaleString()}</span>
                    <span style={{ fontSize: 12, color: '#86868B' }}>
                      最近 {new Date(a.lastSeenAt).toLocaleString()} · 触发 {a.fireCount} 次
                    </span>
                  </Space>
                ),
              },
              {
                title: '操作',
                width: 280,
                fixed: 'right',
                render: (_, a) => {
                  const topoHost = hostOf(a);
                  const openTopology = () => {
                    if (!topoHost) return;
                    navigate(
                      `/op-topology?host=${encodeURIComponent(topoHost)}&fp=${encodeURIComponent(a.fingerprint)}`,
                    );
                  };
                  return (
                    <Space size={0} wrap>
                      <Button size="small" type="link" onClick={() => setViewing(a)}>
                        详情
                      </Button>
                      {a.state === 'firing' && (
                        <Button
                          size="small"
                          type="link"
                          icon={<BugOutlined />}
                          loading={diagnose.isPending}
                          onClick={() => diagnose.mutate(a.fingerprint)}
                        >
                          开排障
                        </Button>
                      )}
                      <Dropdown
                        trigger={['click']}
                        menu={{
                          items: isSilenced(a)
                            ? [{ key: 'unsilence', label: '取消静默' }]
                            : SILENCE_MENU.map((m) => ({ key: m.key, label: m.label })),
                          onClick: ({ key }) => {
                            if (key === 'unsilence') {
                              unsilence.mutate(a.fingerprint);
                            } else {
                              silence.mutate({ fp: a.fingerprint, duration: key as AlarmSilenceDuration });
                            }
                          },
                        }}
                      >
                        <Button size="small" type="link">
                          静默
                        </Button>
                      </Dropdown>
                      <Tooltip
                        title={topoHost ? '在运维关系拓扑中定位该节点' : '该告警缺少主机/实例信息，无法定位拓扑'}
                      >
                        <span style={{ display: 'inline-block' }}>
                          <Button size="small" type="link" disabled={!topoHost} onClick={openTopology}>
                            查看拓扑
                          </Button>
                        </span>
                      </Tooltip>
                    </Space>
                  );
                },
              },
            ]}
          />
        )}
      </Card>

      {/* 详情 Drawer */}
      <Drawer
        width={640}
        open={!!viewing}
        onClose={() => setViewing(null)}
        title={viewing ? `${viewing.alertName} · ${SEVERITY_LABEL[viewing.severity]}` : ''}
      >
        {viewing && (
          <>
            {viewing.severity === 'critical' && viewing.state === 'firing' && (
              <Alert type="error" showIcon message="严重告警，请优先处理" style={{ marginBottom: 12 }} />
            )}
            {isSilenced(viewing) && (
              <Alert
                type="info"
                showIcon
                message={`静默至 ${new Date(viewing.silencedUntil!).toLocaleString()}`}
                description="静默期内不会自动创建排障会话，仍可手动开排障。"
                style={{ marginBottom: 12 }}
              />
            )}
            {viewing.suppressedBy && (
              <Alert
                type="info"
                showIcon
                message={viewing.suppressedBy === 'inhibited' ? '该告警被上游 Alertmanager 抑制' : '该告警处于上游 Alertmanager 静默中'}
                description="上游抑制 / 静默期间不会自动创建排障会话与工单，解除后自动恢复采集。"
                style={{ marginBottom: 12 }}
              />
            )}

            <Card size="small" title="告警时间线" style={{ marginBottom: 12 }}>
              <Timeline items={timelineItems} />
            </Card>

            <Card size="small" title="基本信息" style={{ marginBottom: 12 }}>
              <Descriptions size="small" column={2} bordered>
                <Descriptions.Item label="告警名称">
                  <Typography.Text strong>{viewing.alertName}</Typography.Text>
                </Descriptions.Item>
                <Descriptions.Item label="等级">
                  <StatusPill tone={SEVERITY_TONE[viewing.severity]} dot>
                    {SEVERITY_LABEL[viewing.severity]}
                  </StatusPill>
                </Descriptions.Item>
                <Descriptions.Item label="状态">
                  <StatusPill tone={STATE_TONE[viewing.state]} dot>
                    {STATE_LABEL[viewing.state]}
                  </StatusPill>
                </Descriptions.Item>
                <Descriptions.Item label="来源">
                  {SOURCE_LABEL[viewing.source ?? 'prometheus'] ?? viewing.source}
                </Descriptions.Item>
                <Descriptions.Item label="指纹">
                  <Typography.Text code copyable style={{ fontSize: 11 }}>
                    {viewing.fingerprint}
                  </Typography.Text>
                </Descriptions.Item>
                <Descriptions.Item label="实例 / 主机" span={2}>
                  {viewing.instance || '-'} {viewing.host && `· ${viewing.host}`}
                </Descriptions.Item>
                <Descriptions.Item label="现象" span={2}>
                  {viewing.summary || '-'}
                </Descriptions.Item>
                <Descriptions.Item label="首次出现">
                  {new Date(viewing.firstSeenAt).toLocaleString()}
                </Descriptions.Item>
                <Descriptions.Item label="最近采集">
                  {new Date(viewing.lastSeenAt).toLocaleString()}
                </Descriptions.Item>
                <Descriptions.Item label="触发次数">{viewing.fireCount}</Descriptions.Item>
                <Descriptions.Item label="连续未采集">{viewing.missCount}</Descriptions.Item>
              </Descriptions>
            </Card>

            <Card size="small" title="告警标签（Prometheus labels）" style={{ marginBottom: 12 }}>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {Object.entries(viewing.labels ?? {}).map(([k, v]) => (
                  <Tag key={k} style={{ margin: 0 }}>
                    <span style={{ color: '#86868B' }}>{k}</span>=<span>{v}</span>
                  </Tag>
                ))}
              </div>
            </Card>

            {(viewing.diagSessionId || viewing.ticketId) && (
              <Card size="small" title="自动排障 / 工单" style={{ marginBottom: 12 }}>
                <Descriptions size="small" column={1} bordered>
                  <Descriptions.Item label="排障会话">
                    {viewing.diagSessionId ? (
                      <Space>
                        <Tag color="blue">{viewing.diagSessionId}</Tag>
                        <Button size="small" type="link" onClick={() => navigate('/diagnosis')}>
                          去排障工作台
                        </Button>
                      </Space>
                    ) : (
                      <Typography.Text type="secondary">未触发</Typography.Text>
                    )}
                  </Descriptions.Item>
                  {viewing.diagStatus && (
                    <Descriptions.Item label="排障状态">
                      <StatusPill tone={DIAG_TONE[viewing.diagStatus] ?? 'neutral'} dot>
                        {DIAG_LABEL[viewing.diagStatus] ?? viewing.diagStatus}
                      </StatusPill>
                      {viewing.confidence != null && viewing.confidence > 0 && (
                        <Typography.Text type="secondary" style={{ marginLeft: 8 }}>
                          置信度 {Math.round(viewing.confidence * 100)}%
                        </Typography.Text>
                      )}
                    </Descriptions.Item>
                  )}
                  <Descriptions.Item label="关联工单">
                    {viewing.ticketId ? (
                      <Space>
                        <Tag color="blue">{viewing.ticketId}</Tag>
                        <Button size="small" type="link" onClick={() => navigate('/tickets')}>
                          去工单列表
                        </Button>
                      </Space>
                    ) : (
                      <Typography.Text type="secondary">未建单</Typography.Text>
                    )}
                  </Descriptions.Item>
                  {viewing.lastDiagAt && (
                    <Descriptions.Item label="上次排障">
                      {new Date(viewing.lastDiagAt).toLocaleString()}
                    </Descriptions.Item>
                  )}
                </Descriptions>
              </Card>
            )}

            <Space wrap>
              {viewing.state === 'firing' && (
                <Button
                  type="primary"
                  icon={<BugOutlined />}
                  loading={diagnose.isPending}
                  onClick={() => {
                    diagnose.mutate(viewing.fingerprint);
                    setViewing(null);
                  }}
                >
                  强制开排障
                </Button>
              )}
              {isSilenced(viewing) ? (
                <Button icon={<ClockCircleOutlined />} onClick={() => unsilence.mutate(viewing.fingerprint)}>
                  取消静默
                </Button>
              ) : (
                <Dropdown
                  trigger={['click']}
                  menu={{
                    items: SILENCE_MENU.map((m) => ({ key: m.key, label: m.label })),
                    onClick: ({ key }) =>
                      silence.mutate({ fp: viewing.fingerprint, duration: key as AlarmSilenceDuration }),
                  }}
                >
                  <Button icon={<ClockCircleOutlined />}>静默</Button>
                </Dropdown>
              )}
              <Button icon={<ReloadOutlined />} onClick={() => poll.mutate()}>
                立即同步
              </Button>
            </Space>
          </>
        )}
      </Drawer>

      {/* 通知出站记录 Drawer（T11） */}
      <Drawer
        width={760}
        open={notifyOpen}
        onClose={() => setNotifyOpen(false)}
        title={
          <Space>
            <BellOutlined />
            <span>告警通知出站记录</span>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              仅记录最近 200 条
            </Typography.Text>
          </Space>
        }
        extra={
          <Space>
            <Button size="small" icon={<ReloadOutlined />} onClick={() => notifications.refetch()}>
              刷新
            </Button>
            <Button
              size="small"
              type="primary"
              icon={<SendOutlined />}
              loading={notifyTest.isPending}
              onClick={() => notifyTest.mutate()}
            >
              发送测试
            </Button>
          </Space>
        }
      >
        {notifications.data && !notifications.data.enabled && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="通知出站未启用"
            description="配置环境变量 ALARM_NOTIFY_URL（可选 ALARM_NOTIFY_TOKEN / ALARM_NOTIFY_TIMEOUT_SEC）后重启后端，新告警将自动 POST 到该地址。"
          />
        )}
        {notifications.data?.enabled && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="通知出站已启用"
            description="仅在告警首次出现、死灰复燃或上游解除静默时推送，持续告警不会重复打扰。"
          />
        )}
        <Table
          size="small"
          rowKey="id"
          loading={notifications.isLoading}
          dataSource={notifications.data?.items ?? []}
          pagination={{ pageSize: 10, size: 'small', showTotal: (t) => `共 ${t} 条` }}
          locale={{ emptyText: '暂无投递记录' }}
          expandable={{
            rowExpandable: (r: NotificationLog) => r.alarms.length > 0,
            expandedRowRender: (r: NotificationLog) => (
              <Space direction="vertical" size={4} style={{ width: '100%' }}>
                {r.alarms.map((a) => (
                  <Space key={a.fingerprint} size={8} wrap>
                    <StatusPill tone={SEVERITY_TONE[a.severity as AlarmSeverity] ?? 'neutral'} size="sm" dot>
                      {SEVERITY_LABEL[a.severity as AlarmSeverity] ?? a.severity}
                    </StatusPill>
                    <Typography.Text strong style={{ fontSize: 12 }}>
                      {a.alertName}
                    </Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {a.instance || a.host || '-'}
                    </Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }} ellipsis>
                      {a.summary}
                    </Typography.Text>
                  </Space>
                ))}
              </Space>
            ),
          }}
          columns={[
            {
              title: '时间',
              width: 170,
              dataIndex: 'at',
              render: (v: string) => new Date(v).toLocaleString(),
            },
            {
              title: '类型',
              width: 90,
              dataIndex: 'kind',
              render: (v: NotificationLog['kind']) => (
                <Tag color={v === 'test' ? 'blue' : 'default'} style={{ margin: 0 }}>
                  {v === 'test' ? '测试' : '告警'}
                </Tag>
              ),
            },
            {
              title: '状态',
              width: 100,
              dataIndex: 'status',
              render: (v: NotificationLog['status']) => (
                <Tag color={NOTIFY_STATUS_TAG[v].color} style={{ margin: 0 }}>
                  {NOTIFY_STATUS_TAG[v].text}
                </Tag>
              ),
            },
            {
              title: '告警数',
              width: 80,
              render: (_: unknown, r: NotificationLog) => r.alarms.length,
            },
            {
              title: 'HTTP',
              width: 80,
              dataIndex: 'httpStatus',
              render: (v?: number) => (v ? v : '—'),
            },
            {
              title: '地址 / 错误',
              ellipsis: true,
              render: (_: unknown, r: NotificationLog) => (
                <Space direction="vertical" size={0}>
                  <Typography.Text style={{ fontSize: 12 }} ellipsis>
                    {r.url || '—'}
                  </Typography.Text>
                  {r.error && (
                    <Typography.Text type="danger" style={{ fontSize: 12 }} ellipsis>
                      {r.error}
                    </Typography.Text>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Drawer>
    </>
  );
}
