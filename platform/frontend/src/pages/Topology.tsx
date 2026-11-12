import { useEffect, useRef, useState } from 'react';
import { Graph } from '@antv/g6';
import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Card,
  Col,
  Empty,
  Input,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
} from 'antd';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { AppBrief, Graph as TopoGraph, ViewMode } from '../types';

export default function Topology() {
  const containerRef = useRef<HTMLDivElement>(null);
  const graphRef = useRef<Graph | null>(null);
  const [env, setEnv] = useState('PRO');
  const [date, setDate] = useState<string | undefined>();
  const [viewMode, setViewMode] = useState<ViewMode>('folded');
  const [appId, setAppId] = useState<string | undefined>();
  // 节点标签：null 表示自动（节点少时显示，密集时只悬停显示）
  const [labelsOn, setLabelsOn] = useState<boolean | null>(null);
  const [hoverLabel, setHoverLabel] = useState('');

  const meta = useQuery({ queryKey: ['meta'], queryFn: api.meta });
  const dates = useQuery({ queryKey: ['dates'], queryFn: api.topoDates });
  const apps = useQuery({
    queryKey: ['apps', date, env],
    queryFn: () => api.topoApps(date, env),
  });
  const graph = useQuery({
    queryKey: ['graph', appId, viewMode, date, env],
    queryFn: () => api.topoGraph({ focusAppId: appId, viewMode, date, env }),
  });
  const impact = useQuery({
    queryKey: ['impact', appId, viewMode, date, env],
    queryFn: () => api.topoImpact({ appId, viewMode, date, env }),
    enabled: !!appId,
  });
  const mwTypes = useQuery({
    queryKey: ['mwtypes', date, env],
    queryFn: () => api.topoMwTypes(date, env),
  });
  const diff = useQuery({
    queryKey: ['diff', date, env],
    queryFn: () => api.topoDiff({ to: date, env }),
  });

  useEffect(() => {
    if (meta.data) setEnv(meta.data.defaultEnv);
  }, [meta.data]);

  const nodeCount = graph.data?.nodes.length ?? 0;
  // 节点密集时默认不画标签（否则必然重叠），改为悬停显示完整名称
  const showLabels = labelsOn ?? nodeCount <= 150;

  // G6 v5：数据变化时重建实例（骨架量级下最稳妥）。
  useEffect(() => {
    if (!containerRef.current || !graph.data) return;
    graphRef.current?.destroy();
    const labelById = new Map(graph.data.nodes.map((n) => [n.id, n.label || n.id]));
    const g = new Graph({
      container: containerRef.current,
      autoResize: true,
      data: toG6(graph.data),
      node: {
        style: {
          size: (d: any) => (d.data?.kind === 'app' ? 26 : 16),
          fill: (d: any) => (d.data?.kind === 'app' ? '#1677ff' : '#52c41a'),
          labelText: (d: any) => (showLabels ? shortLabel(d.data?.label ?? d.id) : ''),
          labelFontSize: 10,
          labelPlacement: 'bottom',
          labelFill: '#595959',
          labelMaxWidth: 120,
          labelWordWrap: false,
          labelBackground: true,
          labelBackgroundFill: '#ffffff',
          labelBackgroundOpacity: 0.85,
          labelPadding: [1, 3],
        },
      },
      edge: { style: { stroke: '#d9d9d9', lineWidth: 1 } },
      layout: {
        type: 'force',
        preventOverlap: true,
        nodeSize: 40,
        preventOverlapPadding: 8,
        linkDistance: 160,
      },
      behaviors: ['zoom-canvas', 'drag-canvas', 'drag-element'],
    } as any);
    graphRef.current = g;
    g.on('node:pointerenter', (e: any) => {
      const id = e?.target?.id ?? '';
      setHoverLabel(labelById.get(id) ?? id);
    });
    g.on('node:pointerleave', () => setHoverLabel(''));
    g.render().catch(() => undefined);
    return () => {
      g.destroy();
      graphRef.current = null;
      setHoverLabel('');
    };
  }, [graph.data, showLabels]);

  const appOptions = (apps.data?.apps ?? []).map((a: AppBrief) => ({
    value: a.appId,
    label: `${a.appName || a.appId}（${a.appId}）`,
  }));

  return (
    <>
      <div className="toolbar">
        <Select
          style={{ width: 180 }}
          placeholder="快照日期"
          value={date}
          onChange={setDate}
          allowClear
          options={(dates.data?.dates ?? []).map((d) => ({ value: d, label: d }))}
        />
        <Input
          style={{ width: 110 }}
          value={env}
          onChange={(e) => setEnv(e.target.value.toUpperCase())}
          placeholder="ENV"
        />
        <Segmented
          value={viewMode}
          onChange={(v) => setViewMode(v as ViewMode)}
          options={[
            { label: '折叠（应用）', value: 'folded' },
            { label: '展开（应用+节点）', value: 'expanded' },
          ]}
        />
        <Select
          style={{ width: 320 }}
          showSearch
          allowClear
          placeholder="聚焦应用（ego 网络）"
          value={appId}
          onChange={setAppId}
          optionFilterProp="label"
          options={appOptions}
        />
        <StatusPill tone="info">节点 {graph.data?.nodes.length ?? 0}</StatusPill>
        <StatusPill tone="ok">边 {graph.data?.edges.length ?? 0}</StatusPill>
        {graph.data?.truncated && (
          <StatusPill tone="warn" dot>
            已截断
          </StatusPill>
        )}
      </div>

      <Row gutter={12}>
        <Col xs={24} xl={15}>
          <Card
            size="small"
            title={`拓扑图 · ${graph.data?.syncDate ?? '-'} · ${viewMode}`}
            extra={
              <Space size={8}>
                <span style={{ color: '#8c8c8c', fontSize: 12 }}>{hoverLabel}</span>
                <Space size={4}>
                  <Switch size="small" checked={showLabels} onChange={setLabelsOn} />
                  <span style={{ fontSize: 12, color: '#8c8c8c' }}>节点标签</span>
                </Space>
              </Space>
            }
            styles={{ body: { padding: 4 } }}
          >
            <div ref={containerRef} className="graph-panel" />
            {graph.isLoading && <Spin style={{ marginTop: 8 }} />}
            {!graph.isLoading && (graph.data?.nodes.length ?? 0) === 0 && (
              <Empty description="无拓扑数据" />
            )}
            {graph.error && <Alert type="error" message={(graph.error as Error).message} />}
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card size="small" title="影响面（爆炸半径）" style={{ marginBottom: 12 }}>
            {!appId && <Empty description="在上方选择一个应用" />}
            {impact.data && (
              <>
                <Space wrap style={{ marginBottom: 8 }}>
                  <StatusPill tone={impact.data.totalApps >= 10 ? 'risk' : 'info'}>
                    受影响应用 {impact.data.totalApps}
                  </StatusPill>
                  <StatusPill tone="neutral">口径 {impact.data.level}</StatusPill>
                  {impact.data.note && <StatusPill tone="warn">{impact.data.note}</StatusPill>}
                </Space>
                <Table
                  size="small"
                  rowKey="appKey"
                  pagination={{ pageSize: 8, size: 'small' }}
                  dataSource={impact.data.apps}
                  columns={[
                    { title: '应用', dataIndex: 'appName', ellipsis: true },
                    { title: 'appId', dataIndex: 'appId', ellipsis: true },
                    {
                      title: '目标',
                      dataIndex: 'isTarget',
                      width: 64,
                      render: (v: boolean) =>
                        v ? (
                          <StatusPill tone="info" size="sm">
                            T
                          </StatusPill>
                        ) : null,
                    },
                  ]}
                />
              </>
            )}
            {impact.error && <Alert type="error" message={(impact.error as Error).message} />}
          </Card>
        </Col>
      </Row>

      <Card size="small" style={{ marginTop: 12 }}>
        <Tabs
          size="small"
          items={[
            {
              key: 'mw',
              label: '中间件分布',
              children: (
                <Table
                  size="small"
                  rowKey="type"
                  pagination={false}
                  dataSource={mwTypes.data?.types ?? []}
                  columns={[
                    { title: '类型', dataIndex: 'type' },
                    { title: '依赖行数', dataIndex: 'rows' },
                    { title: '实例数', dataIndex: 'instances' },
                    { title: '应用数', dataIndex: 'apps' },
                  ]}
                />
              ),
            },
            {
              key: 'diff',
              label: `快照差异（${diff.data?.from || '-'} → ${diff.data?.to || '-'}）`,
              children: (
                <>
                  {diff.error && (
                    <Alert type="warning" showIcon message={(diff.error as Error).message} />
                  )}
                  <Row gutter={12}>
                    <Col span={12}>
                      <Card size="small" title={`新增依赖 ${diff.data?.added.length ?? 0}`}>
                        <Table
                          size="small"
                          rowKey={(r) => `${r.appKey}|${r.middlewareType}|${r.connectAddr}|add`}
                          pagination={{ pageSize: 6, size: 'small' }}
                          dataSource={diff.data?.added ?? []}
                          columns={[
                            { title: '应用', dataIndex: 'appName', ellipsis: true },
                            { title: '类型', dataIndex: 'middlewareType', width: 100 },
                            { title: '地址', dataIndex: 'connectAddr', ellipsis: true },
                          ]}
                        />
                      </Card>
                    </Col>
                    <Col span={12}>
                      <Card size="small" title={`移除依赖 ${diff.data?.removed.length ?? 0}`}>
                        <Table
                          size="small"
                          rowKey={(r) => `${r.appKey}|${r.middlewareType}|${r.connectAddr}|rm`}
                          pagination={{ pageSize: 6, size: 'small' }}
                          dataSource={diff.data?.removed ?? []}
                          columns={[
                            { title: '应用', dataIndex: 'appName', ellipsis: true },
                            { title: '类型', dataIndex: 'middlewareType', width: 100 },
                            { title: '地址', dataIndex: 'connectAddr', ellipsis: true },
                          ]}
                        />
                      </Card>
                    </Col>
                  </Row>
                </>
              ),
            },
          ]}
        />
      </Card>
    </>
  );
}

/** 标签截断：避免长地址把画布铺满，完整名称在悬停提示里显示 */
function shortLabel(s: string, max = 16) {
  const str = String(s ?? '');
  return str.length > max ? `${str.slice(0, max - 1)}…` : str;
}

function toG6(g: TopoGraph) {
  return {
    nodes: g.nodes.map((n) => ({
      id: n.id,
      data: { label: n.label, kind: n.kind, type: n.type, depApps: n.depApps },
    })),
    edges: g.edges.map((e) => ({
      id: `${e.source}->${e.target}`,
      source: e.source,
      target: e.target,
      data: { kind: e.kind },
    })),
  };
}
