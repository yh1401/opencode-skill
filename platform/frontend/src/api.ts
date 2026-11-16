import type {
  AlarmEvent,
  AlarmListParams,
  AlarmListResp,
  AlarmPollResp,
  AlarmSilenceDuration,
  AlarmStats,
  AlarmTrendResp,
  AppBrief,
  AuditFinding,
  AuditRun,
  BizOverview,
  BizRule,
  BizScanResult,
  CallLog,
  ChangePlan,
  ChangeTarget,
  ChangeTemplate,
  DegradationOverview,
  DegradationScanResult,
  DiagnosisSession,
  Finding,
  Graph,
  Health,
  Impact,
  InspectionRun,
  InspectionTask,
  KpiResult,
  KnowledgeCase,
  KnowledgeStats,
  MCPTool,
  MCPServerStatus,
  Meta,
  MwTypeStat,
  NotificationLog,
  NotificationResp,
  OpGraph,
  OpListResp,
  OpMwTargetsResp,
  OpNodeDetail,
  OpSyncLogs,
  OpSyncState,
  QualityOverview,
  QualityReport,
  RelationOverride,
  RelationProposal,
  RelationScanResult,
  RelationStats,
  RuleDef,
  RuleOverride,
  Step,
  Ticket,
  TopoDiff,
  User,
  ViewMode,
} from './types';

const BASE = '/api';

const TOKEN_KEY = 'aiops-auth-token';

/** 登录令牌本地存储：请求头注入与 401 清理共用。 */
export const authToken = {
  get: () => localStorage.getItem(TOKEN_KEY) ?? '',
  set: (token: string) => localStorage.setItem(TOKEN_KEY, token),
  clear: () => localStorage.removeItem(TOKEN_KEY),
};

function authHeaders(): Record<string, string> {
  const token = authToken.get();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...authHeaders(), ...(init?.headers ?? {}) },
  });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      /* keep default */
    }
    if (res.status === 401 && !path.startsWith('/auth/login')) {
      authToken.clear();
      window.dispatchEvent(new Event('aiops:unauthorized'));
    }
    throw new Error(message);
  }
  return (await res.json()) as T;
}

export const api = {
  health: () => request<Health>('/health'),
  meta: () => request<Meta>('/meta'),

  // 登录鉴权
  authLogin: (username: string, password: string) =>
    request<{ token: string; user: User }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  authMe: () => request<User>('/auth/me'),
  authLogout: () => request<{ ok: boolean }>('/auth/logout', { method: 'POST' }),
  authChangePassword: (oldPassword: string, newPassword: string) =>
    request<{ token: string; user: User }>('/auth/password', {
      method: 'POST',
      body: JSON.stringify({ oldPassword, newPassword }),
    }),

  // 用户管理
  userList: (role?: string) =>
    request<{ total: number; users: User[] }>(`/users${role ? `?role=${encodeURIComponent(role)}` : ''}`),
  userCreate: (body: {
    username: string;
    displayName?: string;
    password?: string;
    role?: string;
    enabled?: boolean;
  }) => request<User>('/users', { method: 'POST', body: JSON.stringify(body) }),
  userUpdate: (
    id: string,
    body: Partial<{ displayName: string; role: string; enabled: boolean; password: string }>,
  ) => request<User>(`/users/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  userDelete: (id: string) => request<{ ok: boolean }>(`/users/${id}`, { method: 'DELETE' }),

  topoDates: () => request<{ source: string; dates: string[] }>('/topo/dates'),
  topoApps: (date?: string, env?: string) =>
    request<{ date: string; env: string; total: number; apps: AppBrief[] }>(
      `/topo/apps?${qs({ date, env })}`,
    ),
  topoGraph: (p: { focusAppId?: string; viewMode?: ViewMode; date?: string; env?: string }) =>
    request<Graph>(`/topo/graph?${qs(p)}`),
  topoImpact: (p: {
    appId?: string;
    mwType?: string;
    addr?: string;
    viewMode?: ViewMode;
    date?: string;
    env?: string;
  }) => request<Impact>(`/topo/impact?${qs(p)}`),
  topoDiff: (p: { from?: string; to?: string; env?: string }) =>
    request<TopoDiff>(`/topo/diff?${qs(p)}`),
  topoMwTypes: (date?: string, env?: string) =>
    request<{ date: string; env: string; types: MwTypeStat[] }>(`/topo/mwtypes?${qs({ date, env })}`),

  // 运维关系拓扑（tprel 能力面）
  opGraph: (p: Record<string, unknown>) => request<OpGraph>(`/topo/op/graph?${qs(p)}`),
  opDetail: (p: Record<string, unknown>) => request<OpNodeDetail>(`/topo/op/detail?${qs(p)}`),
  opList: (p: Record<string, unknown>) => request<OpListResp>(`/topo/op/list?${qs(p)}`),
  opMwTypes: (p: Record<string, unknown>) =>
    request<{ date: string; env: string; types: string[] }>(`/topo/op/middlewareTypes?${qs(p)}`),
  opMwTargets: (p: Record<string, unknown> = {}) =>
    request<OpMwTargetsResp>(`/topo/op/mwTargets?${qs(p)}`),
  opSync: () =>
    request<{ started: boolean; supported?: boolean; message?: string; state?: OpSyncState }>(
      '/topo/op/sync',
      { method: 'POST' },
    ),
  opSyncStatus: () => request<OpSyncState>('/topo/op/sync/status'),
  opSyncLogs: (p: Record<string, unknown> = {}) => request<OpSyncLogs>(`/topo/op/sync/logs?${qs(p)}`),

  inspectTasks: () => request<{ total: number; tasks: InspectionTask[] }>('/inspect/tasks'),
  inspectRules: () => request<{ total: number; rules: RuleDef[] }>('/inspect/rules'),
  inspectUpdateTask: (id: string, body: Partial<InspectionTask>) =>
    request<InspectionTask>(`/inspect/tasks/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  createTask: (body: Partial<InspectionTask>) =>
    request<InspectionTask>('/inspect/tasks', { method: 'POST', body: JSON.stringify(body) }),
  deleteTask: (id: string) =>
    request<{ deleted: string }>(`/inspect/tasks/${id}`, { method: 'DELETE' }),
  runTask: (id: string) =>
    request<InspectionRun>(`/inspect/tasks/${id}/run`, { method: 'POST' }),
  runAdhoc: (body: { name?: string; scope?: Record<string, unknown> }) =>
    request<InspectionRun>('/inspect/run', { method: 'POST', body: JSON.stringify(body) }),
  inspectRuns: (limit = 50) =>
    request<{ total: number; runs: InspectionRun[] }>(`/inspect/runs?limit=${limit}`),
  inspectRun: (id: string) => request<InspectionRun>(`/inspect/runs/${id}`),
  findings: (runId: string) =>
    request<{ total: number; findings: Finding[] }>(`/inspect/runs/${runId}/findings`),
  report: async (runId: string) => {
    const res = await fetch(`${BASE}/inspect/runs/${runId}/report`, { headers: authHeaders() });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.text();
  },

  createDiagnosis: (body: {
    tenantId?: string;
    appId?: string;
    middlewareType?: string;
    addr?: string;
    symptom?: string;
    env?: string;
    date?: string;
    viewMode?: ViewMode;
    logPath?: string;
  }) =>
    request<DiagnosisSession>('/diagnose/sessions', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  diagnoseSessions: (limit = 50) =>
    request<{ total: number; sessions: DiagnosisSession[] }>(`/diagnose/sessions?limit=${limit}`),
  diagnoseSession: (id: string) => request<DiagnosisSession>(`/diagnose/sessions/${id}`),
  diagnoseFeedback: (
    id: string,
    body: { accurate: boolean; actualRootCause?: string; note?: string; operator?: string },
  ) =>
    request<DiagnosisSession>(`/diagnose/sessions/${id}/feedback`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  mcpProbe: () => request<{ servers: MCPServerStatus[] }>('/mcp/probe'),
  mcpTools: () => request<{ tools: MCPTool[] }>('/mcp/tools'),
  mcpCall: (body: { server: string; tool: string; args?: Record<string, unknown> }) =>
    request<{ latencyMs: number; result: unknown }>('/mcp/call', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  mcpLogs: (limit = 100) =>
    request<{ total: number; logs: CallLog[] }>(`/mcp/logs?limit=${limit}`),

  // 外部日志分析服务（log_analyzer）：由后端探测 /api/health，避免浏览器跨域
  logAnalyzerHealth: () =>
    request<{
      enabled: boolean;
      url: string;
      reachable: boolean;
      status: number;
      latencyMs: number;
      detail: string;
      hint: string;
    }>('/logs/health'),

  // 变更方案 / 风险评估
  changeTemplates: () => request<{ total: number; templates: ChangeTemplate[] }>('/changes/templates'),
  changeGenerate: (body: ChangeTarget) =>
    request<ChangePlan>('/changes/generate', { method: 'POST', body: JSON.stringify(body) }),
  changeList: (limit = 50) => request<{ total: number; plans: ChangePlan[] }>(`/changes?limit=${limit}`),
  changeGet: (id: string) => request<ChangePlan>(`/changes/${id}`),

  // 工单：动作式接口，version 用于并发冲突校验
  ticketList: (limit = 100) => request<{ total: number; tickets: Ticket[] }>(`/tickets?limit=${limit}`),
  ticketGet: (id: string) => request<Ticket>(`/tickets/${id}`),
  ticketClaim: (id: string, version?: number) =>
    request<Ticket>(`/tickets/${id}/claim`, {
      method: 'POST',
      body: JSON.stringify({ version }),
    }),
  ticketAssign: (id: string, assigneeId: string, version?: number, note?: string) =>
    request<Ticket>(`/tickets/${id}/assign`, {
      method: 'POST',
      body: JSON.stringify({ assigneeId, version, note }),
    }),
  ticketStart: (id: string, version?: number) =>
    request<Ticket>(`/tickets/${id}/start`, {
      method: 'POST',
      body: JSON.stringify({ version }),
    }),
  ticketResolve: (id: string, note?: string, version?: number) =>
    request<Ticket>(`/tickets/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ note, version }),
    }),
  ticketClose: (id: string, note?: string, version?: number) =>
    request<Ticket>(`/tickets/${id}/close`, {
      method: 'POST',
      body: JSON.stringify({ note, version }),
    }),
  ticketReopen: (id: string, note?: string, version?: number) =>
    request<Ticket>(`/tickets/${id}/reopen`, {
      method: 'POST',
      body: JSON.stringify({ note, version }),
    }),

  // 资源稽核（CMDB 对账 + Prometheus 佐证）
  auditRun: (params: { date?: string; env?: string; maxHosts?: number }) => {
    const q = new URLSearchParams();
    if (params.date) q.set('date', params.date);
    if (params.env) q.set('env', params.env);
    if (params.maxHosts) q.set('maxHosts', String(params.maxHosts));
    return request<AuditRun>(`/audit/run?${q.toString()}`, { method: 'POST' });
  },
  auditRuns: (limit = 20) => request<{ total: number; runs: AuditRun[] }>(`/audit/runs?limit=${limit}`),
  auditFindings: (runId: string) =>
    request<{ total: number; findings: AuditFinding[] }>(`/audit/runs/${runId}/findings`),

  // 告警中心
  alarmList: (p: AlarmListParams = {}) => {
    const q = qs({
      state: p.state,
      severity: p.severity,
      q: p.q,
      page: p.page,
      pageSize: p.pageSize,
    });
    return request<AlarmListResp>(`/alarms${q ? `?${q}` : ''}`);
  },
  alarmStats: () => request<AlarmStats>('/alarms/stats'),
  alarmTrend: (hours = 24) => request<AlarmTrendResp>(`/alarms/trend?hours=${hours}`),
  alarmGet: (fp: string) => request<AlarmEvent>(`/alarms/${encodeURIComponent(fp)}`),
  alarmPoll: () => request<AlarmPollResp>('/alarms/poll', { method: 'POST' }),
  alarmDegradationScan: () =>
    request<DegradationScanResult>('/alarms/degradation/scan', { method: 'POST' }),
  alarmDegradation: () => request<DegradationOverview>('/alarms/degradation'),
  alarmNotifications: (limit = 50) =>
    request<NotificationResp>(`/alarms/notifications?limit=${limit}`),
  alarmNotifyTest: () =>
    request<NotificationLog>('/alarms/notifications/test', { method: 'POST' }),
  alarmDiagnose: (fp: string) =>
    request<DiagnosisSession>(`/alarms/${encodeURIComponent(fp)}/diagnose`, { method: 'POST' }),
  alarmSilence: (fp: string, duration: AlarmSilenceDuration, reason?: string) =>
    request<AlarmEvent>(`/alarms/${encodeURIComponent(fp)}/silence`, {
      method: 'POST',
      body: JSON.stringify({ duration, reason }),
    }),
  alarmUnsilence: (fp: string) =>
    request<AlarmEvent>(`/alarms/${encodeURIComponent(fp)}/unsilence`, { method: 'POST' }),
  metricsKpi: (days = 7) => request<KpiResult>(`/metrics/kpi?days=${days}`),

  // 外部业务接口监控
  bizOverview: () => request<BizOverview>('/biz-monitor/overview'),
  bizRules: () => request<{ total: number; rules: BizRule[] }>('/biz-monitor/rules'),
  bizCreateRule: (body: Partial<BizRule>) =>
    request<BizRule>('/biz-monitor/rules', { method: 'POST', body: JSON.stringify(body) }),
  bizDeleteRule: (id: string) =>
    request<{ deleted: string }>(`/biz-monitor/rules/${id}`, { method: 'DELETE' }),
  bizScan: () => request<BizScanResult>('/biz-monitor/scan', { method: 'POST' }),

  // 质差分析
  qualityOverview: (appId?: string) => request<QualityOverview>(`/quality/overview?${qs({ appId })}`),
  qualityScan: (appId: string, windowMin: number) =>
    request<QualityReport>('/quality/scan', {
      method: 'POST',
      body: JSON.stringify({ appId, windowMin }),
    }),
  qualityReports: (appId?: string, limit = 20) =>
    request<{ total: number; reports: QualityReport[] }>(`/quality/reports?${qs({ appId, limit })}`),

  // 关系修正（T16）
  relationScan: (body: { from?: string; to?: string; env?: string } = {}) =>
    request<RelationScanResult>('/relations/scan', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  relationList: (p: { status?: string; kind?: string } = {}) =>
    request<{ total: number; proposals: RelationProposal[] }>(`/relations?${qs(p)}`),
  relationStats: () => request<RelationStats>('/relations/stats'),
  relationOverrides: () =>
    request<{ total: number; overrides: RelationOverride[] }>('/relations/overrides'),
  relationUpdate: (id: string, status: 'approved' | 'rejected', note?: string) =>
    request<RelationProposal>(`/relations/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ status, note }),
    }),
  relationApply: (id: string) =>
    request<RelationProposal>(`/relations/${id}/apply`, { method: 'POST' }),

  // 历史故障知识库（T17）
  knowledgeList: (p: { q?: string; appId?: string; limit?: number } = {}) =>
    request<{ total: number; cases: KnowledgeCase[] }>(`/knowledge?${qs(p)}`),
  knowledgeStats: () => request<KnowledgeStats>('/knowledge/stats'),
  knowledgeSimilar: (p: { appId?: string; symptom?: string; limit?: number } = {}) =>
    request<{ total: number; cases: KnowledgeCase[] }>(`/knowledge/similar?${qs(p)}`),
  knowledgeGet: (id: string) => request<KnowledgeCase>(`/knowledge/${id}`),
  knowledgeReindex: () =>
    request<{ indexed: number }>('/knowledge/reindex', { method: 'POST' }),
};

function qs(params: Record<string, unknown>): string {
  const sp = new URLSearchParams();
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') sp.set(k, String(v));
  });
  return sp.toString();
}

export interface StreamHandlers {
  onStep?: (step: Step) => void;
  onSession?: (meta: {
    id: string;
    status: string;
    confidence: number;
    rootCause: string;
    totalSteps: number;
  }) => void;
  onDone?: (session: DiagnosisSession) => void;
  onError?: (message: string) => void;
}

/** 订阅排障会话的证据链增量（SSE）。返回取消函数。 */
export function streamDiagnosis(id: string, handlers: StreamHandlers): () => void {
  // EventSource 不能自定义请求头，登录令牌通过查询参数传递
  const token = authToken.get();
  const es = new EventSource(
    `${BASE}/diagnose/sessions/${id}/stream${token ? `?token=${encodeURIComponent(token)}` : ''}`,
  );
  let closed = false;

  es.addEventListener('step', (e) => {
    try {
      handlers.onStep?.(JSON.parse((e as MessageEvent).data) as Step);
    } catch {
      /* ignore malformed frame */
    }
  });
  es.addEventListener('session', (e) => {
    try {
      handlers.onSession?.(JSON.parse((e as MessageEvent).data));
    } catch {
      /* ignore */
    }
  });
  es.addEventListener('done', (e) => {
    try {
      handlers.onDone?.(JSON.parse((e as MessageEvent).data) as DiagnosisSession);
    } catch {
      /* ignore */
    }
    es.close();
    closed = true;
  });
  es.addEventListener('timeout', () => {
    handlers.onError?.('等待超时，请重新查询会话');
    es.close();
    closed = true;
  });
  es.onerror = () => {
    if (closed) return;
    handlers.onError?.('SSE 连接中断');
    es.close();
    closed = true;
  };

  return () => {
    closed = true;
    es.close();
  };
}
