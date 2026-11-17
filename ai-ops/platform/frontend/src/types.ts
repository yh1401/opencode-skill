// 与后端 internal/domain/models.go 的 JSON 契约保持一致。

export type ViewMode = 'folded' | 'expanded';

export interface AppBrief {
  appId: string;
  appName: string;
  depProjects: string[];
  nodes: string[];
  mwTypes: string[];
  depApps: number;
}

export interface GraphNode {
  id: string;
  label: string;
  kind: 'app' | 'middleware';
  type?: string;
  appId?: string;
  addr?: string;
  depApps: number;
  degree: number;
  env?: string;
}

export interface GraphEdge {
  source: string;
  target: string;
  kind: string;
}

export interface Graph {
  syncDate: string;
  viewMode: ViewMode;
  env: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
  truncated: boolean;
}

export interface ImpactApp {
  appKey: string;
  appId: string;
  appName: string;
  isTarget: boolean;
  depProject?: string;
}

export interface ImpactMW {
  type: string;
  addr: string;
  depApps: number;
  isTarget: boolean;
}

export interface Impact {
  syncDate: string;
  level: 'app' | 'middleware';
  target: string;
  apps: ImpactApp[];
  middlewares: ImpactMW[];
  totalApps: number;
  note?: string;
}

export interface DiffItem {
  appKey: string;
  appId: string;
  appName: string;
  middlewareType: string;
  connectAddr: string;
  depProject?: string;
}

export interface TopoDiff {
  from: string;
  to: string;
  added: DiffItem[];
  removed: DiffItem[];
}

export interface MwTypeStat {
  type: string;
  rows: number;
  instances: number;
  apps: number;
}

export interface TopoRow {
  id: number;
  apolloHostName: string;
  apolloHostAddr: string;
  apolloAppId: string;
  apolloAppName: string;
  appDept: string;
  appOwner: string;
  node: string;
  middlewareType: string;
  connectAddr: string;
  middlewareDb: string;
  env: string;
  syncDate: string;
}

export interface Scope {
  appIds: string[];
  mwTypes: string[];
  env: string;
  date?: string;
}

export interface InspectionTask {
  id: string;
  name: string;
  scene: string;
  scope: Scope;
  ruleSetVersion: string;
  cron: string;
  enabled: boolean;
  createdAt: string;
}

export interface RunStats {
  items: number;
  risk: number;
  notTriggered: number;
  noData: number;
  notApplicable: number;
  error: number;
}

export interface InspectionRun {
  id: string;
  taskId: string;
  taskName: string;
  trigger: string;
  topoSyncDate: string;
  viewMode: ViewMode;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'partial';
  stats: RunStats;
  message?: string;
  startedAt: string;
  finishedAt: string;
}

export type Verdict = 'RISK' | 'NOT_TRIGGERED' | 'NO_DATA' | 'NOT_APPLICABLE';

export interface Finding {
  id: string;
  runId: string;
  ruleCode: string;
  title: string;
  severity: string;
  verdict: Verdict;
  objectType: string;
  objectKey: string;
  detail: string;
  evidence?: Record<string, unknown>;
  impact?: Record<string, unknown>;
  createdAt: string;
}

export interface Step {
  no: number;
  source: string;
  tool: string;
  title: string;
  status: 'ok' | 'warn' | 'error' | 'skipped';
  args?: Record<string, unknown>;
  result?: Record<string, unknown>;
  latencyMs: number;
  error?: string;
  at: string;
}

export interface Candidate {
  title: string;
  reason: string;
  confidence: number;
  source: string;
}

export interface DiagnosisFeedback {
  accurate: boolean;
  actualRootCause?: string;
  note?: string;
  operator?: string;
  at: string;
}

// 数据源验真结果（五步流程第 2 步）：先确认数据源本身可信，再解释信号。
export interface SourceHealth {
  source: string; // promthus | es | cmdb | pinpoint
  status: 'ok' | 'degraded' | 'unavailable' | 'skipped';
  detail?: string;
  checks?: string[];
  at: string;
}

// 问题单（五步流程第 2 步产出）：实体 / 级别 / 信号来源 / 时间窗 / 影响面 / 可信度。
export interface TicketEntity {
  appId?: string;
  appName?: string;
  middlewareType?: string;
  addr?: string;
  ip?: string;
  hostName?: string;
  node?: string;
  owner?: string;
}

export interface TicketImpact {
  hosts: number;
  apps: number;
  middlewares: number;
  hostList?: string[];
  appNames?: string[];
  nodes?: string[];
}

export interface DiagnosisTicket {
  entity: TicketEntity;
  severity: 'critical' | 'warning' | 'info';
  signals: string[];
  window: { start: string; end: string; minutes: number };
  impact: TicketImpact;
  confidence: 'confirmed' | 'degraded' | 'no_data';
  reason?: string;
}

export interface DiagnosisSession {
  id: string;
  tenantId: string;
  appId?: string;
  middlewareType?: string;
  addr?: string;
  symptom: string;
  status: 'open' | 'investigating' | 'root_caused' | 'no_data' | 'failed';
  rootCause?: string;
  confidence: number;
  candidates?: Candidate[];
  impact?: Impact;
  steps: Step[];
  sourceHealth?: SourceHealth[];
  ticket?: DiagnosisTicket;
  createdAt: string;
  finishedAt: string;
  feedback?: DiagnosisFeedback;
}

export interface MCPTool {
  server: string;
  name: string;
  description: string;
  inputSchema?: Record<string, unknown>;
  allowed: boolean;
}

export interface MCPServerStatus {
  name: string;
  url: string;
  enabled: boolean;
  reachable: boolean;
  tools: number;
  error?: string;
  latencyMs: number;
}

export interface StoreInfo {
  dir: string;
  files: number;
  postgres: string;
}

export interface Health {
  status: string;
  uptime: string;
  storage: StoreInfo;
  topo: string;
  mcp: boolean;
  dataMode: string;
  now: string;
}

export interface Meta {
  dataMode: string;
  defaultEnv: string;
  defaultViewMode: ViewMode;
  explosionThreshold: number;
  topo: { mode: string; source: string; dir: string; dates: string[] };
  storage: StoreInfo;
  mcp: { enabled: boolean; servers: string[]; urls: Record<string, string> };
}

export interface CallLog {
  server: string;
  tool: string;
  args?: Record<string, unknown>;
  ok: boolean;
  error?: string;
  latencyMs: number;
  at: string;
}

// —— 运维关系拓扑（tprel 能力面，与后端 internal/domain/optopo.go 契约一致）——

export interface OpGraphHost {
  id: number;
  name: string;
  host: string;
  appCount: number;
}

export interface OpGraphNode {
  id: string;
  category: 'application' | 'middleware';
  name: string;
  label: string;
  node: string;
  apolloAppId: string;
  apolloAppName: string;
  appDept: string;
  appOwner: string;
  engProject: string;
  depProject: string;
  deployProjectName?: string;
  ppApp: string;
  engProjects?: string[];
  depProjects?: string[];
  ppApps?: string[];
  nodes?: string[];
  apolloHosts: OpGraphHost[];
  appIds: string[];
  mwType: string;
  addr: string;
  mwProject: string;
  depCount: number;
}

export interface OpGraphEdge {
  source: string;
  target: string;
  kind: string;
}

export interface OpGraphCounts {
  apolloHosts: number;
  application: number;
  middleware: number;
  edges: number;
  nodes: number;
}

export interface OpGraph {
  nodes: OpGraphNode[];
  edges: OpGraphEdge[];
  syncDate: string;
  env: string;
  partial: boolean;
  apolloHosts: OpGraphHost[];
  dataSource: string;
  counts: OpGraphCounts;
}

export interface OpNodeDetailMw {
  mwType: string;
  addr: string;
  mwDb: string;
  mwProject: string;
  depCount: number;
}

export interface OpNodeDetailApp {
  nodeId: string;
  apolloAppId: string;
  apolloAppName: string;
  appDept: string;
  appOwner: string;
  engProject: string;
  depProject: string;
  node: string;
  ppApp: string;
  apolloHosts: OpGraphHost[];
}

export interface OpNodeDetail {
  id: string;
  category: 'application' | 'middleware' | 'host';
  name: string;
  syncDate: string;
  env: string;
  apolloAppId?: string;
  apolloAppName?: string;
  appDept?: string;
  appOwner?: string;
  engProject?: string;
  depProject?: string;
  node?: string;
  ppApp?: string;
  engProjects?: string[];
  depProjects?: string[];
  ppApps?: string[];
  nodes?: string[];
  mwType?: string;
  addr?: string;
  mwDb?: string;
  mwProject?: string;
  depCount?: number;
  hostId?: number;
  hostName?: string;
  hostAddr?: string;
  hostAppCount?: number;
  apolloHosts: OpGraphHost[];
  middlewares: OpNodeDetailMw[];
  apps: OpNodeDetailApp[];
}

export interface OpListRow {
  id: number;
  apolloHostName: string;
  apolloAppId: string;
  apolloAppName: string;
  appDept: string;
  appOwner: string;
  node: string;
  engProject: string;
  depProject: string;
  ppApplication: string;
  middlewareType: string;
  connectAddr: string;
  middlewareDb: string;
  middlewareProject: string;
  env: string;
  syncDate: string;
  depProjects?: string[];
  nodes?: string[];
  engProjects?: string[];
  ppApplications?: string[];
}

export interface OpListResp {
  page: number;
  pageSize: number;
  pageTotal: number;
  list: OpListRow[];
  date?: string;
  env?: string;
}

export interface OpSyncStats {
  hosts: number;
  apps: number;
  middleware: number;
  relations: number;
}

export interface OpSyncResult {
  hostCount?: number;
  availableHosts?: number;
  appCount?: number;
  namespaceCount?: number;
  requestCount?: number;
  insertCount?: number;
  partial?: boolean;
  unfinishedApps?: string[];
  newRelations?: string[];
  lostRelations?: string[];
  relationRows?: number;
  relationWrote?: boolean;
  relationErr?: boolean;
  relationMsg?: string;
  triggerBy?: string;
  startTime?: string;
  endTime?: string;
  costMs?: number;
  resultText?: string;
  totalRelations?: number;
  typeStats?: Record<string, number>;
  nodeStats?: Record<string, number>;
  newCount?: number;
  lostCount?: number;
  prevDate?: string;
  noMiddlewareCount?: number;
  noMiddlewareApps?: { appId: string; appName: string }[];
}

export interface OpSyncState {
  running: boolean;
  supported?: boolean;
  triggerBy?: string;
  startTime?: string;
  endTime?: string;
  costMs?: number;
  error?: string;
  stageText?: string;
  percent?: number;
  stats?: OpSyncStats;
  result?: OpSyncResult | null;
}

// —— 排障工作台用的中间件候选（opMwTargets）——

export interface OpMwTarget {
  mwType: string;
  addr: string;
  depApps: number;
  sampleApps?: string[];
}

export interface OpMwTargetsResp {
  date: string;
  env: string;
  targets: OpMwTarget[];
}

export interface OpSyncLogInfo {
  date: string;
  file: string;
  sizeBytes: number;
}

export interface OpSyncLogs {
  supported?: boolean;
  message?: string;
  date?: string;
  file?: string;
  sizeBytes?: number;
  content?: string;
  truncated?: boolean;
  files?: OpSyncLogInfo[];
}

// —— 变更方案 / 风险评估 ——

export interface ChangeStep {
  no: number;
  title: string;
  checkTool: string;
  actionHint: string;
  rollback: string;
}

export interface ChangeTemplate {
  id: string;
  name: string;
  targetType: string;
  defaultEnv: string;
  description: string;
  steps: ChangeStep[];
}

export interface ChangeTarget {
  templateId: string;
  targetType: string;
  appId?: string;
  mwType?: string;
  mwAddr?: string;
  datePrev: string;
  dateToday: string;
  env: string;
  notes?: string;
}

export interface RiskResult {
  score: number;
  level: 'low' | 'medium' | 'high' | 'critical';
  blockers: string[];
  explosionApps: number;
  driftRatio: number;
  recentFaults: number;
  notes?: string;
}

export interface ChangePlan {
  id: string;
  templateId: string;
  templateName: string;
  target: ChangeTarget;
  steps: ChangeStep[];
  risk: RiskResult;
  createdAt: string;
  createdBy: string;
}

// —— 隐患与工单 ——

export type TicketPriority = 'P0' | 'P1' | 'P2' | 'P3';
export type TicketStatus = 'open' | 'assigned' | 'in_progress' | 'resolved' | 'closed';

/** 工单操作留痕：谁、何时、做了什么、状态如何变化 */
export interface TicketEvent {
  at: string;
  actorId: string;
  actorName: string;
  action: string;
  from?: TicketStatus;
  to?: TicketStatus;
  note?: string;
}

export interface Ticket {
  id: string;
  sourceType: string;
  sourceId: string;
  title: string;
  appId?: string;
  mwType?: string;
  addr?: string;
  confidence: number;
  status: TicketStatus;
  priority: TicketPriority;
  symptom: string;
  rootCause: string;
  suggestion?: string;
  assignee?: string;
  assigneeId?: string;
  findings: string[];
  evidenceRef: string;
  /** 发现时间：来源排障会话创建时间 */
  detectedAt?: string;
  /** 认领 / 指派时间，用于 MTTA */
  assignedAt?: string;
  /** 开始处置时间 */
  startedAt?: string;
  createdAt: string;
  updatedAt: string;
  /** 处置完成时间：标记「已处置」时写入 */
  resolvedAt?: string;
  closedAt?: string;
  reopenCount?: number;
  version?: number;
  history?: TicketEvent[];
  mttaSeconds?: number;
  mttrSeconds?: number;
}

// —— 登录鉴权与用户 ——

export type Role = 'admin' | 'ops' | 'viewer';

export interface User {
  id: string;
  username: string;
  displayName: string;
  role: Role;
  source: 'local' | 'corp';
  externalId?: string;
  enabled: boolean;
  createdAt: string;
  lastLoginAt?: string;
}


// —— 巡检规则配置化 ——

export interface RuleParam {
  key: string;
  label: string;
  type: 'number' | 'boolean' | 'enum';
  default: any;
  min?: number;
  max?: number;
  enum?: any[];
}

export interface RuleDef {
  code: string;
  name: string;
  severity: string;
  description: string;
  params: RuleParam[];
  builtin: boolean;
  enabled: boolean;
}

export interface RuleOverride {
  enabled: boolean;
  params?: Record<string, any>;
}

export interface InspectionTask {
  id: string;
  name: string;
  scene: string;
  scope: { appIds: string[]; mwTypes: string[]; env: string; date?: string };
  ruleSetVersion: string;
  cron: string;
  enabled: boolean;
  createdAt: string;
  rules?: Record<string, RuleOverride>;
}

// —— 资源稽核（CMDB 对账） ——

export interface AuditFinding {
  id: string;
  runId: string;
  checkCode: 'AUDIT_C1' | 'AUDIT_C2' | 'AUDIT_C3' | 'AUDIT_C4';
  objectType: string;
  objectKey: string;
  severity: string;
  verdict: 'RISK' | 'NOT_TRIGGERED' | 'NO_DATA' | 'NOT_APPLICABLE';
  detail: string;
  evidence?: Record<string, any>;
  createdAt: string;
}

export interface AuditStats {
  items: number;
  risk: number;
  notTriggered: number;
  noData: number;
  notApplicable: number;
}

// —— 告警中心（Prometheus 告警接入 / 自动排障 / 自动建单）——

export type AlarmSeverity = 'critical' | 'warning' | 'info' | 'other';
export type AlarmState = 'firing' | 'resolved';
export type AlarmDiagStatus = 'open' | 'investigating' | 'root_caused' | 'failed';

export interface AlarmEvent {
  fingerprint: string;
  source?: 'prometheus' | 'degradation' | 'alertmanager' | 'inspection' | 'biz' | 'quality';
  alertName: string;
  instance: string;
  host: string;
  severity: AlarmSeverity;
  labels: Record<string, string>;
  summary: string;
  state: AlarmState;
  firstSeenAt: string;
  lastSeenAt: string;
  fireCount: number;
  missCount: number;
  lastDiagAt?: string;
  diagSessionId?: string;
  diagStatus?: AlarmDiagStatus;
  confidence?: number;
  ticketId?: string;
  ticketStatus?: string;
  resolvedAt?: string;
  silencedUntil?: string;
  silenceReason?: string;
  /** 上游 Alertmanager 抑制原因：silenced | inhibited */
  suppressedBy?: string;
  // —— 二次关联（列表接口 merge）——
  sessionStatus?: string;
  rootCause?: string;
  sessionCreatedAt?: string;
  sessionFinishedAt?: string;
  ticketPriority?: string;
  ticketCreatedAt?: string;
  ticketUpdatedAt?: string;
  ticketClosedAt?: string;
}

export interface AlarmStatsBucket {
  firing: number;
  resolved: number;
}

export interface AlarmStats {
  stats: Record<AlarmSeverity, AlarmStatsBucket>;
  total: number;
  firing: number;
  autoDiag: number;
  autoTickets24h: number;
}

export interface AlarmListResp {
  total: number;
  page: number;
  pageSize: number;
  items: AlarmEvent[];
}

export interface AlarmListParams {
  state?: AlarmState;
  severity?: AlarmSeverity;
  q?: string;
  page?: number;
  pageSize?: number;
}

export interface AlarmTrendPoint {
  hour: string; // yyyy-mm-ddTHH
  created: number;
  resolved: number;
}

export interface AlarmTrendResp {
  points: AlarmTrendPoint[];
}

export type AlarmSilenceDuration = '30m' | '2h' | '24h' | 'forever';

export interface AlarmPollResp {
  upserted: number;
  autoDiag: number;
}

// —— 指标劣化独立页（T8）——

export interface DegradationScanResult {
  scanned: number;
  degraded: string[];
  created: number;
  resolved: number;
  autoDiag: number;
  windowMin: number;
  thresholdPct: number;
  at: string;
  error?: string;
}

export interface DegradationOverview {
  lastScan: DegradationScanResult | null;
  firing: AlarmEvent[];
  resolved: AlarmEvent[];
  affectedHosts: number;
}

// —— 外部业务接口监控 ——

export type BizMetric = 'error_rate' | 'request_total' | 'tp99';
export type BizComparator = 'gt' | 'lt';

export interface BizRule {
  id: string;
  name: string;
  appId: string;
  metric: BizMetric;
  expr: string;
  comparator: BizComparator;
  threshold: number;
  severity: 'critical' | 'warning' | 'info';
  enabled: boolean;
  windowMin: number;
  createdAt: string;
  updatedAt: string;
}

export interface BizItem {
  ruleId: string;
  ruleName: string;
  appId: string;
  metric: BizMetric;
  value: number;
  threshold: number;
  comparator: BizComparator;
  breach: boolean;
  severity: string;
  error?: string;
}

export interface BizScanResult {
  at: string;
  rules: number;
  breaches: number;
  created: number;
  items: BizItem[];
}

export interface BizOverview {
  rules: BizRule[];
  ruleCount: number;
  lastScan: BizScanResult | null;
  firing: AlarmEvent[];
}

// —— 质差分析 ——

export interface QualityMetric {
  name: BizMetric;
  baseline: number;
  recent: number;
  growthPct: number;
  points: number;
}

export interface QualityRootCause {
  type: 'database' | 'cache' | 'mq' | 'gateway' | 'application';
  target: string;
  addr: string;
  score: number;
  reason: string;
}

export interface QualityReport {
  id: string;
  appId: string;
  at: string;
  windowMin: number;
  level: 'normal' | 'suspected' | 'degraded';
  score: number;
  metrics: QualityMetric[];
  rootCause: QualityRootCause | null;
  evidence: string[];
  alarmFingerprint?: string;
}

export interface QualityScanResult {
  at: string;
  appId: string;
  reportId: string;
  level: 'normal' | 'suspected' | 'degraded';
  score: number;
  breaches: number;
}

export interface QualityOverview {
  latest: QualityReport | null;
  history: QualityReport[];
  firing: AlarmEvent[];
}

// —— 告警通知出站（T11）——

export interface NotificationItem {
  fingerprint: string;
  alertName: string;
  instance: string;
  host: string;
  severity: string;
  summary: string;
  source?: string;
}

export interface NotificationLog {
  id: string;
  kind: 'alarm' | 'test';
  status: 'sent' | 'failed' | 'skipped';
  url: string;
  httpStatus?: number;
  error?: string;
  alarms: NotificationItem[];
  at: string;
}

export interface NotificationResp {
  enabled: boolean;
  items: NotificationLog[];
}

// —— 考核指标（S3）——

export interface KpiLatency {
  p50: number; // 秒
  p90: number; // 秒
  avg: number;
  samples: number;
  target: number;
  rate: number; // 达标率 0–1
}

export interface KpiCounts {
  alarms: number;
  firing: number;
  resolved: number;
  withDiag: number;
  withTicket: number;
  sessions: number;
  tickets: number;
  runs: number;
}

export interface KpiAutomation {
  autoDiagRate: number;
  autoTicketRate: number;
  inspectCronRate: number;
  diagSuccessRate: number;
}

export interface KpiAccuracy {
  labeled: number;
  accurate: number;
  rate: number;
  coverage: number;
}

export interface KpiDailyPoint {
  date: string; // yyyy-mm-dd
  alarms: number;
  sessions: number;
  tickets: number;
  runs: number;
}

export interface KpiResult {
  startAt: string;
  endAt: string;
  days: number;
  counts: KpiCounts;
  alertToSession: KpiLatency;
  sessionToRootCause: KpiLatency;
  rootCauseToTicket: KpiLatency;
  alertToTicket: KpiLatency;
  automation: KpiAutomation;
  accuracy: KpiAccuracy;
  bySource: Record<string, number>;
  daily: KpiDailyPoint[];
  note: string;
}

// —— 资源稽核（CMDB 对账）——

export interface AuditRun {
  id: string;
  date: string;
  env: string;
  status: 'running' | 'succeeded' | 'failed';
  stats: AuditStats;
  message?: string;
  sources: string[];
  startedAt: string;
  finishedAt: string;
}

// —— 关系修正（T16）——

export type RelationKind = 'missing' | 'stale' | 'orphan';
export type RelationStatus = 'pending' | 'approved' | 'rejected' | 'applied';

export interface RelationProposal {
  id: string;
  fingerprint: string;
  kind: RelationKind;
  appId?: string;
  appName?: string;
  middlewareType?: string;
  connectAddr?: string;
  depProject?: string;
  source: 'topo_diff' | 'audit';
  detail: string;
  suggestion: string;
  autoFix: boolean;
  status: RelationStatus;
  note?: string;
  fromDate?: string;
  toDate?: string;
  createdAt: string;
  updatedAt: string;
  appliedAt?: string;
}

export interface RelationScanResult {
  from: string;
  to: string;
  added: number;
  removed: number;
  orphans: number;
  created: number;
  skipped: number;
  total: number;
  at: string;
  warnings?: string[];
}

export interface RelationStats {
  pending: number;
  approved: number;
  rejected: number;
  applied: number;
  autoFix: number;
  manualOnly: number;
  total: number;
}

export interface RelationOverride {
  fingerprint: string;
  kind: RelationKind;
  action: 'add' | 'remove' | 'manual';
  appId?: string;
  appName?: string;
  middlewareType?: string;
  connectAddr?: string;
  depProject?: string;
  proposalId: string;
  fromDate?: string;
  toDate?: string;
  appliedAt: string;
}

// —— 历史故障知识库（T17）——

export interface KnowledgeEvidence {
  no: number;
  source: string;
  title: string;
  status: string;
  error?: string;
  latencyMs: number;
}

export interface KnowledgeCase {
  id: string;
  title: string;
  appId?: string;
  middlewareType?: string;
  addr?: string;
  symptom: string;
  rootCause?: string;
  confidence: number;
  status: string;
  steps: number;
  evidence?: KnowledgeEvidence[];
  ticketId?: string;
  ticketStatus?: string;
  reviewed: boolean;
  accurate: boolean;
  actualRootCause?: string;
  keywords?: string[];
  createdAt: string;
  finishedAt?: string;
  score?: number;
}

export interface KnowledgeAppCount {
  appId: string;
  count: number;
}

export interface KnowledgeStats {
  total: number;
  apps: number;
  reviewed: number;
  accurate: number;
  accuracy: number;
  topApps: KnowledgeAppCount[] | null;
}
