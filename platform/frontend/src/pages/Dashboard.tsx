import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Alert, Card, Col, Descriptions, Row, Spin, Statistic, Table, Typography } from 'antd';
import { AlertOutlined, FileTextOutlined } from '@ant-design/icons';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import PlatformMeta from '../components/PlatformMeta';

export default function Dashboard() {
  const navigate = useNavigate();
  const health = useQuery({ queryKey: ['health'], queryFn: api.health, refetchInterval: 5000 });
  const meta = useQuery({ queryKey: ['meta'], queryFn: api.meta });
  const mw = useQuery({
    queryKey: ['mwtypes', meta.data?.defaultEnv],
    queryFn: () => api.topoMwTypes(undefined, meta.data?.defaultEnv),
    enabled: !!meta.data,
  });
  const probe = useQuery({ queryKey: ['probe'], queryFn: api.mcpProbe, refetchInterval: 15000 });
  // 告警中心：当前告警与近 24h 自动建单
  const alarms = useQuery({
    queryKey: ['dashboard-alarms'],
    queryFn: api.alarmStats,
    refetchInterval: 15000,
  });

  if (health.isLoading || meta.isLoading) return <Spin />;
  if (health.error) return <Alert type="error" message={`后端不可用：${(health.error as Error).message}`} />;

  const h = health.data!;
  const m = meta.data!;
  const totals = (mw.data?.types ?? []).reduce(
    (acc, t) => ({
      rows: acc.rows + t.rows,
      instances: acc.instances + t.instances,
      apps: acc.apps + t.apps,
    }),
    { rows: 0, instances: 0, apps: 0 },
  );

  return (
    <>
      <Row gutter={12} className="stat-grid">
        <Col flex="1 1 200px">
          <Card size="small">
            <Statistic title="拓扑快照" value={m.topo.dates.length} suffix="个日期" />
            <div className="mono">{m.topo.dates.join(' / ')}</div>
          </Card>
        </Col>
        <Col flex="1 1 200px">
          <Card size="small">
            <Statistic title="中间件实例" value={totals.instances} suffix={`/ ${totals.rows} 行依赖`} />
          </Card>
        </Col>
        <Col flex="1 1 200px">
          <Card size="small">
            <Statistic title="依赖应用" value={totals.apps} suffix="个" />
          </Card>
        </Col>
        <Col flex="1 1 200px">
          <Card size="small">
            <Statistic title="运行时长" value={h.uptime} />
            <div className="mono">files={h.storage.files} · pg={h.storage.postgres}</div>
          </Card>
        </Col>
        <Col flex="1 1 200px">
          <Card
            size="small"
            hoverable
            style={{ cursor: 'pointer' }}
            onClick={() => navigate('/alert-center')}
          >
            <Statistic
              title="当前告警"
              value={alarms.data?.firing ?? 0}
              suffix="条"
              valueStyle={{ color: (alarms.data?.firing ?? 0) > 0 ? '#C41E1A' : undefined }}
              prefix={<AlertOutlined />}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              严重 {alarms.data?.stats.critical.firing ?? 0} · 警告{' '}
              {alarms.data?.stats.warning.firing ?? 0}
            </Typography.Text>
          </Card>
        </Col>
        <Col flex="1 1 200px">
          <Card
            size="small"
            hoverable
            style={{ cursor: 'pointer' }}
            onClick={() => navigate('/alert-center')}
          >
            <Statistic
              title="近 24h 自动建单"
              value={alarms.data?.autoTickets24h ?? 0}
              suffix="张"
              prefix={<FileTextOutlined />}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              自动排障中 {alarms.data?.autoDiag ?? 0} 条
            </Typography.Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={12}>
        <Col xs={24} lg={12}>
          <Card size="small" title="平台运行态" style={{ marginBottom: 12 }}>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="服务状态">
                <StatusPill tone="ok" dot>{h.status}</StatusPill>
              </Descriptions.Item>
              <Descriptions.Item label="topo 数据源">
                <span className="mono">{h.topo}</span>
              </Descriptions.Item>
              <Descriptions.Item label="topo 模式">{m.topo.mode}</Descriptions.Item>
              <Descriptions.Item label="本地存储">
                <span className="mono">{m.storage.dir}</span>（{m.storage.files} 个文件）
              </Descriptions.Item>
              <Descriptions.Item label="PG 备份">
                <StatusPill tone={m.storage.postgres === 'connected' ? 'ok' : 'warn'} dot>
                  {m.storage.postgres}
                </StatusPill>
              </Descriptions.Item>
              <Descriptions.Item label="默认口径">
                env={m.defaultEnv} / viewMode={m.defaultViewMode} / 爆炸半径阈值=
                {m.explosionThreshold}
              </Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card size="small" title="上游服务探针" style={{ marginBottom: 12 }}>
            <Table
              size="small"
              rowKey="name"
              pagination={false}
              dataSource={probe.data?.servers ?? []}
              columns={[
                { title: '服务', dataIndex: 'name' },
                {
                  title: '状态',
                  dataIndex: 'reachable',
                  render: (v: boolean, r) =>
                    v ? (
                      <StatusPill tone="ok" size="sm" dot>可达 · {r.tools} 工具</StatusPill>
                    ) : (
                      <StatusPill tone="risk" size="sm" dot>不可达</StatusPill>
                    ),
                },
                { title: '耗时', dataIndex: 'latencyMs', render: (v: number) => `${v}ms` },
                {
                  title: '地址',
                  dataIndex: 'url',
                  render: (v: string) => <span className="mono">{v}</span>,
                },
              ]}
            />
            {probe.data?.servers.some((s) => !s.reachable) && (
              <Alert
                style={{ marginTop: 8 }}
                type="warning"
                showIcon
                message="上游服务不可达不会阻塞平台启动，排障会退化为拓扑 + 规则推断"
              />
            )}
          </Card>
        </Col>
      </Row>

      <Card size="small" title="中间件类型分布（当前快照）">
        <Table
          size="small"
          rowKey="type"
          pagination={false}
          loading={mw.isLoading}
          dataSource={mw.data?.types ?? []}
          columns={[
            { title: '类型', dataIndex: 'type' },
            { title: '依赖行数', dataIndex: 'rows', sorter: (a, b) => a.rows - b.rows },
            { title: '实例数', dataIndex: 'instances', sorter: (a, b) => a.instances - b.instances },
            { title: '应用数', dataIndex: 'apps', sorter: (a, b) => a.apps - b.apps },
          ]}
        />
      </Card>

      <PlatformMeta />
    </>
  );
}
