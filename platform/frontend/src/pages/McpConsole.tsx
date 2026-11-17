import { useMemo, useState, type MouseEvent as ReactMouseEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Divider,
  Drawer,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tooltip,
} from 'antd';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import { TOOL_DOCS, type ToolDoc } from '../data/mcpToolDocs';
import type { MCPTool } from '../types';

// 服务级口径：上游组成、时间单位、源码基线工具数。用于探针表与调用面板提示。
const SERVER_PROFILE: Record<
  string,
  { upstream: string; timeHint: string; baseline: number; note: string }
> = {
  pinpoint: {
    upstream: 'Pinpoint Web · EasyViews · Prometheus',
    timeHint: '时间窗参数用毫秒（from_ts / to_ts）；进程内直连 Prometheus 的取数用秒',
    baseline: 25,
    note: '基础 4 + 架构分析 21；EasyViews 拓扑与 prometheus 侧同名工具互为镜像，需指定唯一权威源',
  },
  prometheus: {
    upstream: 'Prometheus / Thanos · EasyViews',
    timeHint: 'start_time / end_time 支持 Unix 秒、RFC3339 与相对时长（如 1h）',
    baseline: 36,
    note: '36 为并集口径：list_stores 仅在 thanos 后端注册，all + prometheus 为 35',
  },
  cmdb: {
    upstream: 'CMDB 资产库',
    timeHint: '无时间窗参数；分页 pageSize ≤ 100',
    baseline: 5,
    note: '平台侧只放行 cmdb_server_query，入参须为内网 IP 或主机名',
  },
  es: {
    upstream: 'Elasticsearch',
    timeHint: '时间范围用日期数学（如 now-15m）或 ISO 时间',
    baseline: 5,
    note: '5 个工具全部只读；字段名须与索引 mapping 一致，否则判定为 NO_DATA 且不报错',
  },
};

// 工具分组：按能力域归类，便于在工具清单里筛选。
const GROUPS: Record<string, { name: string; tools: string[] }[]> = {
  pinpoint: [
    {
      name: '基础查询',
      tools: [
        'list_applications_tool',
        'get_server_map_tool',
        'drag_heatmap_tool',
        'get_transaction_trace_tool',
      ],
    },
    {
      name: '拓扑结构',
      tools: [
        'list_nodes_tool',
        'get_module_topology_tool',
        'get_link_topology_tool',
        'get_host_structure_tool',
        'get_project_deploy_tool',
        'analyze_link_topology_tool',
        'analyze_host_concentration_tool',
      ],
    },
    {
      name: '架构规则',
      tools: [
        'arch_rule_check_tool',
        'list_arch_rules_tool',
        'arch_label_probe_tool',
        'analyze_node_runtime_tool',
        'analyze_nodes_runtime_tool',
        'analyze_pinpoint_dependencies_tool',
        'summarize_architecture_findings_tool',
      ],
    },
    {
      name: '范围与映射',
      tools: ['resolve_analysis_scope_tool', 'filter_dep_projects_tool', 'build_app_mapping_tool'],
    },
    { name: '指标', tools: ['prometheus_query_tool', 'arch_metric_query_tool'] },
    { name: '异步任务', tools: ['arch_task_open_tool', 'arch_task_status_tool'] },
  ],
  prometheus: [
    {
      name: '查询',
      tools: [
        'query',
        'range_query',
        'exemplar_query',
        'series',
        'label_names',
        'label_values',
        'metric_metadata',
      ],
    },
    { name: '告警与规则', tools: ['list_alerts', 'alertmanagers', 'list_rules'] },
    {
      name: '目标与健康',
      tools: ['targets_metadata', 'list_targets', 'healthy', 'ready', 'tsdb_stats', 'list_stores'],
    },
    {
      name: '元信息',
      tools: [
        'flags',
        'build_info',
        'config',
        'runtime_info',
        'wal_replay_status',
        'docs_list',
        'docs_read',
        'docs_search',
      ],
    },
    { name: '主机指标', tools: ['host_metrics_realtime', 'host_metrics_trend'] },
    {
      name: '拓扑（EasyViews）',
      tools: [
        'get_module_topology',
        'get_chain_topology',
        'get_node_list',
        'get_virtual_structure',
        'get_project_topology',
      ],
    },
    {
      name: '写操作',
      tools: ['clean_tombstones', 'delete_series', 'snapshot', 'reload', 'quit'],
    },
  ],
  cmdb: [{ name: '资产查询', tools: ['cmdb_server_query'] }],
  es: [
    { name: '索引与分片', tools: ['list_indices', 'get_mappings', 'get_shards'] },
    { name: '检索', tools: ['search', 'esql'] },
  ],
};

function groupOf(server: string, tool: string): string {
  const hit = (GROUPS[server] ?? []).find((g) => g.tools.includes(tool));
  return hit?.name ?? '其他';
}

// 常用参数的口径提示：文档未逐参数写明时用来补「这个参数是干什么的」。
const PARAM_HINTS: Record<string, string> = {
  service_name: '项目组 / 服务名，Pinpoint 默认 DEFAULT',
  application_name: 'Pinpoint 应用名（list_applications_tool 返回的原值）',
  application: 'Pinpoint 应用名',
  trace_id: '事务 ID，来自散点图或服务地图下钻',
  focus_timestamp: '聚焦时间点（毫秒）',
  agent_id: 'Agent 标识，按实例过滤',
  span_id: 'Span 序号，定位调用树中的某一段',
  link_trace_id: '被调方 traceId，用于跨应用串联',
  link_span_id: '被调方 spanId',
  node: '部署节点（形如 云公司->贵州），来自 list_nodes_tool',
  node_prefix: '节点名前缀，用于模糊过滤',
  node_code: '节点编码 ctyun_node_code3',
  projects: '工程名列表（projectName），来自 get_module_topology_tool',
  product_name: '产品名，缺省取节点下全部产品',
  minute: '统计窗口（分钟）',
  caller_range: '上游搜索层数',
  callee_range: '下游搜索层数',
  filter_node: '是否过滤非目标节点',
  node_type: '节点类型过滤',
  task_id: '任务标识，透传到结果，便于串联多步调用',
  dep_suffix_only: '是否只分析 -dep 工程',
  dep_suffix: '依赖工程后缀，默认 -dep',
  rule: '架构规则名，来自 list_arch_rules_tool',
  rules: '规则名列表，批量判定时使用',
  env: '环境标识（如 prod / test）',
  deployment: '部署形态（容器 / 虚拟机）',
  namespace: '命名空间，限定取数范围',
  dep_only: '仅统计 -dep 工程',
  exclude_namespace: '需排除的命名空间',
  window: '统计窗口，支持 15m / 1h 等写法',
  start_ts: '开始时间（Unix 秒）',
  end_ts: '结束时间（Unix 秒）',
  sample_points: '采样点数',
  step_seconds: '采样步长（秒）',
  top_n: '仅返回风险排名前 N 项',
  query: 'PromQL 表达式',
  start_time: '起始时间，支持 Unix 秒 / RFC3339 / 1h',
  end_time: '结束时间，同上',
  step: '查询步长，如 30s',
  time: '评估时间点（Unix 秒）',
  match_target: '目标标签匹配，如 {job="node"}',
  'match[]': '序列匹配表达式列表',
  ip: '主机 IP（内网优先）',
  hostName: '主机名',
  currentPage: '页码，从 1 开始',
  pageSize: '每页条数，≤100',
  state: '资产状态：0 在线 / 1 库存 / 2 计划上线 / 3 维修 / 4 报废',
  index: '索引名，支持通配',
  index_pattern: '索引模式，支持尾部 *',
  query_body: 'Elasticsearch DSL 查询体',
};

function docOf(server: string, tool: string): ToolDoc | undefined {
  return TOOL_DOCS[server]?.[tool];
}

function blankForDocType(type?: string): unknown {
  if (!type) return '';
  if (type.startsWith('list') || type.endsWith('[]')) return [];
  if (type === 'dict' || type === 'object') return {};
  if (type === 'bool' || type === 'boolean') return false;
  if (['int', 'float', 'number', 'integer'].includes(type)) return 0;
  return '';
}

// 文档参数优先（含说明/默认值），缺失时回退到运行时 inputSchema。
function toolParams(tool: MCPTool, doc?: ToolDoc): ParamRow[] {
  if (doc && doc.inputs.length > 0) {
    return doc.inputs.map((p) => ({
      key: p.name,
      type: p.type,
      required: p.required,
      desc: p.desc || PARAM_HINTS[p.name] || '-',
      def: p.def,
    }));
  }
  return schemaParams(tool.inputSchema).map((p) => ({
    ...p,
    desc: p.desc === '-' ? PARAM_HINTS[p.key] ?? '-' : p.desc,
  }));
}

// 依据文档参数生成调用骨架：必填项 + 带默认值的选填项。
function docSampleArgs(doc: ToolDoc): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  doc.inputs.forEach((p) => {
    if (p.def !== undefined && p.def !== '') {
      const raw = p.def;
      if (/^(true|false)$/.test(raw)) out[p.name] = raw === 'true';
      else if (/^-?\d+(\.\d+)?$/.test(raw)) out[p.name] = Number(raw);
      else out[p.name] = raw;
      return;
    }
    if (p.required) out[p.name] = blankForDocType(p.type);
  });
  return out;
}

function sampleOf(tool: MCPTool, doc?: ToolDoc): Record<string, unknown> {
  if (doc && doc.inputs.length > 0) return docSampleArgs(doc);
  return sampleArgs(tool.inputSchema);
}

interface ParamRow {
  key: string;
  type: string;
  required: boolean;
  desc: string;
  def?: string;
  enumValues?: string;
}

// 把 inputSchema 的 properties/required 摊平成表格行，覆盖三种写出风格。
function schemaParams(schema?: Record<string, unknown>): ParamRow[] {
  const props = (schema?.properties ?? {}) as Record<string, Record<string, any>>;
  const required = (schema?.required ?? []) as string[];
  return Object.entries(props).map(([key, raw]) => {
    const p = raw ?? {};
    let type = p.type as string | undefined;
    if (!type) {
      const union = p.anyOf ?? p.oneOf ?? p.allOf;
      if (Array.isArray(union)) {
        type = union.map((u: any) => u?.type ?? 'any').join(' | ');
      }
    }
    return {
      key,
      type: type ?? 'any',
      required: required.includes(key),
      desc: [p.description, p.title].filter(Boolean).join(' — ') || '-',
      def: p.default !== undefined ? JSON.stringify(p.default) : undefined,
      enumValues: Array.isArray(p.enum) ? p.enum.map((v: any) => String(v)).join(' | ') : undefined,
    };
  });
}

function blankOf(type?: string): unknown {
  switch (type) {
    case 'number':
    case 'integer':
      return 0;
    case 'boolean':
      return false;
    case 'array':
      return [];
    case 'object':
      return {};
    default:
      return '';
  }
}

// 依据 schema 生成调用参数骨架：必填项 + 带默认值/枚举的选填项。
function sampleArgs(schema?: Record<string, unknown>): Record<string, unknown> {
  const props = (schema?.properties ?? {}) as Record<string, Record<string, any>>;
  const required = new Set((schema?.required ?? []) as string[]);
  const out: Record<string, unknown> = {};
  Object.entries(props).forEach(([key, raw]) => {
    const p = raw ?? {};
    const hasEnum = Array.isArray(p.enum) && p.enum.length > 0;
    if (!required.has(key) && p.default === undefined && !hasEnum) return;
    if (p.default !== undefined) out[key] = p.default;
    else if (hasEnum) out[key] = p.enum[0];
    else out[key] = blankOf(p.type);
  });
  return out;
}

export default function McpConsole() {
  const qc = useQueryClient();
  const [serverFilter, setServerFilter] = useState<string>('全部');
  const [group, setGroup] = useState<string>('全部');
  const [keyword, setKeyword] = useState('');
  const [detail, setDetail] = useState<{ server: string; tool: MCPTool } | null>(null);
  const [callTarget, setCallTarget] = useState<{ server: string; tool: MCPTool } | null>(null);
  const [callArgs, setCallArgs] = useState('');
  const [result, setResult] = useState<{ ok: boolean; latencyMs: number; body: unknown } | null>(
    null,
  );
  // 「功能说明」列可拖拽调宽：其它列固定，说明列单独记忆宽度
  const [descWidth, setDescWidth] = useState(360);

  const startResize = (e: ReactMouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const startX = e.clientX;
    const startW = descWidth;
    document.body.classList.add('col-resizing');
    const move = (ev: MouseEvent) => {
      setDescWidth(Math.min(760, Math.max(220, startW + (ev.clientX - startX))));
    };
    const up = () => {
      document.body.classList.remove('col-resizing');
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
  };

  const probe = useQuery({ queryKey: ['probe'], queryFn: api.mcpProbe, refetchInterval: 15000 });
  const tools = useQuery({ queryKey: ['mcp-tools'], queryFn: api.mcpTools, refetchInterval: 30000 });
  const logs = useQuery({ queryKey: ['mcp-logs'], queryFn: () => api.mcpLogs(100) });

  const call = useMutation({
    mutationFn: (v: { server: string; tool: string; args: string }) => {
      let args: Record<string, unknown> | undefined;
      if (v.args.trim()) {
        try {
          args = JSON.parse(v.args);
        } catch {
          throw new Error('参数不是合法 JSON');
        }
      }
      return api.mcpCall({ server: v.server, tool: v.tool, args });
    },
    onSuccess: (res) => {
      setResult({ ok: true, latencyMs: res.latencyMs, body: res.result });
      qc.invalidateQueries({ queryKey: ['mcp-logs'] });
    },
    onError: (e: Error) => {
      setResult({ ok: false, latencyMs: 0, body: e.message });
      qc.invalidateQueries({ queryKey: ['mcp-logs'] });
    },
  });

  const allTools = useMemo<MCPTool[]>(() => tools.data?.tools ?? [], [tools.data]);

  const servers = useMemo(() => {
    const names = new Set<string>();
    (probe.data?.servers ?? []).forEach((s) => names.add(s.name));
    allTools.forEach((t) => names.add(t.server));
    return Array.from(names);
  }, [probe.data, allTools]);

  const groupOptions = useMemo(() => {
    const names = new Set<string>();
    allTools.forEach((t) => {
      if (serverFilter !== '全部' && t.server !== serverFilter) return;
      names.add(groupOf(t.server, t.name));
    });
    return [
      { value: '全部', label: `全部分组（${names.size}）` },
      ...Array.from(names).map((g) => ({ value: g, label: g })),
    ];
  }, [serverFilter, allTools]);

  const visibleTools = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    return allTools.filter((t) => {
      if (serverFilter !== '全部' && t.server !== serverFilter) return false;
      if (group !== '全部' && groupOf(t.server, t.name) !== group) return false;
      if (!kw) return true;
      return (
        t.name.toLowerCase().includes(kw) ||
        (t.description ?? '').toLowerCase().includes(kw) ||
        (docOf(t.server, t.name)?.summary ?? '').toLowerCase().includes(kw)
      );
    });
  }, [allTools, serverFilter, group, keyword]);

  const detailDoc = detail ? docOf(detail.server, detail.tool.name) : undefined;
  const detailProfile = detail ? SERVER_PROFILE[detail.server] : undefined;
  const detailParamRows = detail ? toolParams(detail.tool, detailDoc) : [];
  const detailSample = detail ? JSON.stringify(sampleOf(detail.tool, detailDoc), null, 2) : '';

  const callDoc = callTarget ? docOf(callTarget.server, callTarget.tool.name) : undefined;
  const callProfile = callTarget ? SERVER_PROFILE[callTarget.server] : undefined;
  const callRequired = callTarget
    ? toolParams(callTarget.tool, callDoc).filter((p) => p.required).map((p) => p.key)
    : [];

  const openCall = (srv: string, t: MCPTool) => {
    const args = sampleOf(t, docOf(srv, t.name));
    setCallArgs(Object.keys(args).length > 0 ? JSON.stringify(args, null, 2) : '');
    setResult(null);
    setCallTarget({ server: srv, tool: t });
  };

  return (
    <>
      <Card size="small" title="上游服务探针（四服务统一走网关 /mcp）" style={{ marginBottom: 12 }}>
        <Table
          size="small"
          rowKey="name"
          pagination={false}
          loading={probe.isLoading}
          dataSource={probe.data?.servers ?? []}
          columns={[
            { title: '服务', dataIndex: 'name', width: 100 },
            {
              title: '状态',
              dataIndex: 'reachable',
              width: 170,
              render: (v: boolean, r) => {
                if (!v) return <StatusPill tone="risk" size="sm" dot>不可达</StatusPill>;
                const p = SERVER_PROFILE[r.name];
                return (
                  <Space size={4}>
                    <StatusPill tone="ok" size="sm" dot>可达 · {r.tools}</StatusPill>
                    {p && p.baseline !== r.tools && (
                      <Tooltip title={`源码基线 ${p.baseline} 个，当前实例注册 ${r.tools} 个，疑似版本不一致`}>
                        <span>
                          <StatusPill tone="warn" size="sm">基线 {p.baseline}</StatusPill>
                        </span>
                      </Tooltip>
                    )}
                  </Space>
                );
              },
            },
            { title: '耗时', dataIndex: 'latencyMs', width: 80, render: (v: number) => `${v}ms` },
            {
              title: '上游 / 口径',
              dataIndex: 'name',
              render: (name: string) => {
                const p = SERVER_PROFILE[name];
                if (!p) return '-';
                return (
                  <Tooltip title={p.note}>
                    <div>
                      <div>{p.upstream}</div>
                      <div style={{ color: '#8c8c8c', fontSize: 12 }}>{p.timeHint}</div>
                    </div>
                  </Tooltip>
                );
              },
            },
            { title: '地址', dataIndex: 'url', width: 220, render: (v: string) => <span className="mono">{v}</span> },
            { title: '错误', dataIndex: 'error', ellipsis: true, render: (v?: string) => <span className="mono">{v || '-'}</span> },
          ]}
        />
      </Card>

      <Card
        size="small"
        title="工具调用（黑名单拦截 + 审计落盘）"
        style={{ marginBottom: 12 }}
        extra={
          <span style={{ color: '#8c8c8c', fontSize: 12 }}>
            共 {allTools.length} 个 · 当前筛选 {visibleTools.length} 个 · 放行{' '}
            {allTools.filter((t) => t.allowed).length} 个
          </span>
        }
      >
        <Space wrap style={{ marginBottom: 8 }}>
          <Select
            value={serverFilter}
            style={{ width: 170 }}
            onChange={(v) => {
              setServerFilter(v);
              setGroup('全部');
            }}
            options={[
              { value: '全部', label: `全部服务（${allTools.length}）` },
              ...servers.map((s) => ({ value: s, label: s })),
            ]}
          />
          <Input.Search
            allowClear
            placeholder="搜索工具名 / 说明"
            style={{ width: 260 }}
            onChange={(e) => setKeyword(e.target.value)}
          />
          <Select value={group} style={{ width: 200 }} onChange={setGroup} options={groupOptions} />
        </Space>

              <Table
                size="small"
                rowKey={(r: MCPTool) => `${r.server}-${r.name}`}
                scroll={{ x: 820 + descWidth }}
                pagination={{
                  pageSize: 20,
                  size: 'small',
                  showTotal: (total) => `共 ${total} 个`,
                }}
                dataSource={visibleTools}
                columns={[
                  {
                    title: '工具',
                    dataIndex: 'name',
                    width: 240,
                    render: (v: string, r: MCPTool) => (
                      <Space size={4}>
                        <Tooltip title="点击查看详情说明与出入参">
                          <a className="mono" onClick={() => setDetail({ server: r.server, tool: r })}>{v}</a>
                        </Tooltip>
                        {docOf(r.server, r.name)?.star && (
                          <StatusPill tone="warn" size="sm">★</StatusPill>
                        )}
                      </Space>
                    ),
                  },
                  {
                    title: '服务',
                    dataIndex: 'server',
                    width: 110,
                    render: (v: string) => <StatusPill tone="neutral" size="sm">{v}</StatusPill>,
                  },
                  {
                    title: '分组',
                    dataIndex: 'name',
                    width: 130,
                    render: (_: string, r: MCPTool) => (
                      <StatusPill tone="info" size="sm">{groupOf(r.server, r.name)}</StatusPill>
                    ),
                  },
                  {
                    title: (
                      <>
                        功能说明
                        <Tooltip title="拖动右侧竖线调整列宽">
                          <span className="mcp-col-resizer" onMouseDown={startResize} />
                        </Tooltip>
                      </>
                    ),
                    dataIndex: 'description',
                    width: descWidth,
                    onHeaderCell: () => ({ style: { position: 'relative' } }),
                    render: (v: string | undefined, r: MCPTool) => {
                      const text = docOf(r.server, r.name)?.summary || v || '-';
                      return (
                        <Tooltip
                          placement="topLeft"
                          title={<div style={{ maxWidth: 520 }}>{text}</div>}
                        >
                          <div
                            style={{
                              display: '-webkit-box',
                              WebkitLineClamp: 2,
                              WebkitBoxOrient: 'vertical',
                              overflow: 'hidden',
                              whiteSpace: 'normal',
                              lineHeight: 1.5,
                              fontSize: 13,
                            }}
                          >
                            {text}
                          </div>
                        </Tooltip>
                      );
                    },
                  },
                  {
                    key: 'inputs',
                    title: '入参',
                    width: 70,
                    render: (_: unknown, r: MCPTool) =>
                      `${toolParams(r, docOf(r.server, r.name)).length}`,
                  },
                  {
                    key: 'outputs',
                    title: '出参',
                    width: 70,
                    render: (_: unknown, r: MCPTool) => {
                      const d = docOf(r.server, r.name);
                      if (!d) return '-';
                      if (d.outputs.length > 0) return `${d.outputs.length}`;
                      return (
                        <Tooltip title="文档未做字段归一，返回上游原始 JSON">
                          <span style={{ color: '#8c8c8c' }}>原始</span>
                        </Tooltip>
                      );
                    },
                  },
                  {
                    title: '放行',
                    dataIndex: 'allowed',
                    width: 80,
                    render: (v: boolean) =>
                      v ? (
                        <StatusPill tone="ok" size="sm" dot>是</StatusPill>
                      ) : (
                        <StatusPill tone="risk" size="sm" dot>否</StatusPill>
                      ),
                  },
                  {
                    key: 'actions',
                    title: '操作',
                    width: 120,
                    render: (_: unknown, r: MCPTool) => (
                      <Space size={0}>
                        <Button size="small" type="link" onClick={() => setDetail({ server: r.server, tool: r })}>
                          详情
                        </Button>
                        <Button size="small" type="link" onClick={() => openCall(r.server, r)}>
                          调用
                        </Button>
                      </Space>
                    ),
                  },
                ]}
              />

        <Divider style={{ margin: '14px 0 8px' }} />
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            marginBottom: 8,
          }}
        >
          <span style={{ fontWeight: 600 }}>调用审计</span>
          <span style={{ color: '#8c8c8c', fontSize: 12 }}>
            最近 {logs.data?.logs?.length ?? 0} 条 · 展开行可查看入参与错误
          </span>
        </div>
            <Table
              size="small"
              rowKey={(r) => `${r.server}-${r.tool}-${r.at}`}
              loading={logs.isLoading}
              dataSource={logs.data?.logs ?? []}
              pagination={{ pageSize: 10, size: 'small' }}
              expandable={{
                expandedRowRender: (r) => (
                  <pre className="mono" style={{ margin: 0 }}>
                    {JSON.stringify({ args: r.args, error: r.error }, null, 2)}
                  </pre>
                ),
              }}
              columns={[
                {
                  title: '时间',
                  dataIndex: 'at',
                  width: 150,
                  render: (v: string) => new Date(v).toLocaleTimeString(),
                },
                {
                  title: '调用',
                  render: (_, r) => (
                    <Space size={4}>
                      <StatusPill tone="neutral" size="sm">{r.server}</StatusPill>
                      <span className="mono">{r.tool}</span>
                    </Space>
                  ),
                },
                {
                  title: '结果',
                  dataIndex: 'ok',
                  width: 70,
                  render: (v: boolean) =>
                    v ? (
                      <StatusPill tone="ok" size="sm" dot>ok</StatusPill>
                    ) : (
                      <StatusPill tone="risk" size="sm" dot>fail</StatusPill>
                    ),
                },
                { title: '耗时', dataIndex: 'latencyMs', width: 80, render: (v: number) => `${v}ms` },
              ]}
            />
      </Card>

      <Drawer
        width={780}
        open={!!detail}
        onClose={() => setDetail(null)}
        title={
          detail && (
            <Space size={6} wrap>
              <span className="mono">{detail.tool.name}</span>
              <StatusPill tone="neutral" size="sm">{detail.server}</StatusPill>
              <StatusPill tone="info" size="sm">{groupOf(detail.server, detail.tool.name)}</StatusPill>
              {detailDoc?.star && <StatusPill tone="warn" size="sm">★ 故障发现</StatusPill>}
              {!detail.tool.allowed && <StatusPill tone="risk" size="sm">未放行</StatusPill>}
            </Space>
          )
        }
        extra={
          detail && (
            <Button
              type="primary"
              size="small"
              onClick={() => {
                openCall(detail.server, detail.tool);
                setDetail(null);
              }}
            >
              去调用
            </Button>
          )
        }
      >
        {detail && (
          <Space direction="vertical" size={10} style={{ width: '100%' }}>
            <div>
              <div style={{ color: '#8c8c8c', fontSize: 12, marginBottom: 4 }}>功能说明</div>
              <div>{detailDoc?.summary || detail.tool.description || '文档未收录该工具的说明。'}</div>
            </div>

            <Divider style={{ margin: '2px 0' }} />

            <div>
              <div style={{ color: '#8c8c8c', fontSize: 12, marginBottom: 4 }}>
                主要入参（{detailParamRows.length}）· * 为必填
              </div>
              {detailParamRows.length === 0 ? (
                <span style={{ color: '#8c8c8c' }}>无入参，直接调用即可</span>
              ) : (
                <Table
                  size="small"
                  rowKey="key"
                  pagination={false}
                  dataSource={detailParamRows}
                  columns={[
                    {
                      title: '参数',
                      dataIndex: 'key',
                      width: 180,
                      render: (v: string, p: ParamRow) => (
                        <span className="mono">
                          {v}
                          {p.required && <span style={{ color: '#d4380d' }}> *</span>}
                        </span>
                      ),
                    },
                    { title: '类型', dataIndex: 'type', width: 100 },
                    {
                      title: '默认 / 取值',
                      width: 170,
                      render: (_: unknown, p: ParamRow) => p.enumValues ?? p.def ?? '-',
                    },
                    { title: '作用说明', dataIndex: 'desc' },
                  ]}
                />
              )}
              <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 4 }}>
                参数说明取自《上游服务数据契约与工具对照》；运行时 inputSchema 为空时以本表为准。
              </div>
            </div>

            <Divider style={{ margin: '2px 0' }} />

            <div>
              <div style={{ color: '#8c8c8c', fontSize: 12, marginBottom: 4 }}>
                主要出参
                {detailDoc && detailDoc.outputs.length > 0 ? `（${detailDoc.outputs.length}）` : ''}
              </div>
              {detailDoc && detailDoc.outputs.length > 0 ? (
                <Space wrap size={4}>
                  {detailDoc.outputs.map((f) => (
                    <StatusPill key={f} tone="ok" size="sm">{f}</StatusPill>
                  ))}
                </Space>
              ) : (
                <span style={{ color: '#8c8c8c' }}>上游原始 JSON，平台按需解析（未做字段归一）</span>
              )}
              {detailDoc?.returnNote && (
                <Alert
                  style={{ marginTop: 6 }}
                  type="info"
                  showIcon
                  message="返回形态"
                  description={detailDoc.returnNote}
                />
              )}
            </div>

            <Divider style={{ margin: '2px 0' }} />

            <div>
              <div style={{ color: '#8c8c8c', fontSize: 12, marginBottom: 4 }}>调用示例</div>
              <pre className="mono" style={{ margin: 0, background: '#fafafa', padding: 8 }}>
                {detailSample}
              </pre>
            </div>

            {detailProfile && (
              <div style={{ color: '#8c8c8c', fontSize: 12 }}>
                口径：{detailProfile.timeHint}；上游：{detailProfile.upstream}
              </div>
            )}
          </Space>
        )}
      </Drawer>

      <Modal
        open={!!callTarget}
        width={720}
        title={
          callTarget && (
            <Space size={6} wrap>
              <span className="mono">{callTarget.tool.name}</span>
              <StatusPill tone="neutral" size="sm">{callTarget.server}</StatusPill>
              {callDoc?.star && <StatusPill tone="warn" size="sm">★ 故障发现</StatusPill>}
              {!callTarget.tool.allowed && <StatusPill tone="risk" size="sm">未放行</StatusPill>}
            </Space>
          )
        }
        okText="执行调用"
        cancelText="取消"
        confirmLoading={call.isPending}
        onOk={() =>
          callTarget &&
          call.mutate({ server: callTarget.server, tool: callTarget.tool.name, args: callArgs })
        }
        onCancel={() => setCallTarget(null)}
      >
        {callTarget && (
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <div style={{ color: '#8c8c8c', fontSize: 12 }}>
              {callDoc?.summary || callTarget.tool.description || '-'}
            </div>
            {callProfile && (
              <div style={{ color: '#8c8c8c', fontSize: 12 }}>
                口径：{callProfile.timeHint} · 上游：{callProfile.upstream}
              </div>
            )}
            <div style={{ fontSize: 12 }}>
              必填参数：
              <span className="mono">{callRequired.length > 0 ? callRequired.join('、') : '无'}</span>
            </div>
            <div style={{ fontSize: 12 }}>入参 JSON（留空表示无入参）</div>
            <Input.TextArea
              className="mono"
              rows={8}
              value={callArgs}
              onChange={(e) => setCallArgs(e.target.value)}
              placeholder='例如 {"query":"up"}'
            />
            {!callTarget.tool.allowed && (
              <Alert type="warning" showIcon message="该工具不在放行名单内，调用会被网关拦截" />
            )}
            {result && (
              <Alert
                type={result.ok ? 'success' : 'error'}
                showIcon
                message={
                  result.ok ? `调用成功 · ${result.latencyMs}ms` : '调用失败'
                }
                description={
                  <pre className="mono" style={{ margin: 0, maxHeight: 260, overflow: 'auto' }}>
                    {typeof result.body === 'string'
                      ? result.body
                      : JSON.stringify(result.body, null, 2)}
                  </pre>
                }
              />
            )}
          </Space>
        )}
      </Modal>
    </>
  );
}
