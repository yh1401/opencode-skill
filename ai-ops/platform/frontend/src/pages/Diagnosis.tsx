import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  Alert,
  App as AntdApp,
  Badge,
  Button,
  Card,
  Col,
  Descriptions,
  Divider,
  Empty,
  Form,
  Input,
  List,
  Progress,
  Row,
  Segmented,
  Select,
  Space,
  Table,
  Timeline,
  Typography,
} from 'antd';
import { LinkOutlined, SearchOutlined } from '@ant-design/icons';
import { api, streamDiagnosis } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type { DiagnosisSession, Impact, OpMwTarget, Step } from '../types';

const STATUS_TONE: Record<string, PillTone> = {
  open: 'neutral',
  investigating: 'info',
  root_caused: 'risk',
  no_data: 'neutral',
  failed: 'risk',
};

const SEVERITY_TONE: Record<string, PillTone> = {
  critical: 'risk',
  warning: 'warn',
  info: 'info',
};

const HEALTH_TONE: Record<string, PillTone> = {
  ok: 'ok',
  degraded: 'warn',
  unavailable: 'risk',
  skipped: 'neutral',
};

const STEP_COLOR: Record<string, string> = {
  ok: 'green',
  warn: 'orange',
  error: 'red',
  skipped: 'gray',
};

const STEP_TONE: Record<string, PillTone> = {
  ok: 'ok',
  warn: 'warn',
  error: 'risk',
  skipped: 'neutral',
};

function ImpactCard({ impact, level }: { impact?: Impact; level: 'app' | 'middleware' | string }) {
  const navigate = useNavigate();
  if (!impact) return null;
  const mwIDs = (impact.middlewares ?? []).map((m) => `${m.type}:${m.addr}`);
  return (
    <Card
      size="small"
      style={{ marginTop: 8 }}
      title={
        <Space>
          <StatusPill tone={level === 'middleware' ? 'ok' : 'info'}>
            {level === 'middleware' ? '中间件反向查询' : '应用爆炸半径'}
          </StatusPill>
          <span>影响 {impact.totalApps} 个应用</span>
        </Space>
      }
      extra={
        <Space>
          <Button
            size="small"
            type="link"
            icon={<LinkOutlined />}
            onClick={() => {
              if (impact.level === 'middleware') {
                navigate(
                  `/op-topology?expandDepNode=0&env=${impact.syncDate ? '' : 'PRO'}&middlewareType=${encodeURIComponent(impact.target.split(':')[0])}`,
                );
              } else {
                const appId = impact.apps.find((a) => a.isTarget)?.appId || impact.target;
                navigate(`/op-topology?expandDepNode=0&apolloAppId=${encodeURIComponent(appId)}`);
              }
            }}
          >
            查看完整拓扑
          </Button>
          <Button
            size="small"
            type="link"
            icon={<LinkOutlined />}
            onClick={() => navigate(`/topology?appId=${encodeURIComponent(impact.target)}&viewMode=folded`)}
          >
            去影响面分析
          </Button>
        </Space>
      }
    >
      {impact.level === 'middleware' && (
        <>
          <Descriptions size="small" column={2} bordered style={{ marginBottom: 8 }}>
            <Descriptions.Item label="目标中间件" span={2}>
              <StatusPill tone="ok">{impact.target}</StatusPill>
            </Descriptions.Item>
            <Descriptions.Item label="影响应用数">{impact.totalApps}</Descriptions.Item>
            <Descriptions.Item label="快照日期">{impact.syncDate}</Descriptions.Item>
          </Descriptions>
          <Table
            size="small"
            rowKey="appKey"
            pagination={{ pageSize: 8, size: 'small' }}
            dataSource={impact.apps}
            columns={[
              { title: '应用', dataIndex: 'appName', ellipsis: true, render: (v: string) => v || '-' },
              { title: 'appId', dataIndex: 'appId', ellipsis: true },
              {
                title: '操作',
                width: 140,
                render: (_, r) => (
                  <Button
                    size="small"
                    type="link"
                    icon={<SearchOutlined />}
                    onClick={() => {
                      navigate(
                        `/diagnosis?appId=${encodeURIComponent(r.appId)}&env=PRO`,
                      );
                    }}
                  >
                    从此应用排障
                  </Button>
                ),
              },
            ]}
          />
        </>
      )}
      {impact.level === 'app' && (
        <>
          <Descriptions size="small" column={2} bordered style={{ marginBottom: 8 }}>
            <Descriptions.Item label="目标应用">
              <StatusPill tone="info">{impact.target}</StatusPill>
            </Descriptions.Item>
            <Descriptions.Item label="快照日期">{impact.syncDate}</Descriptions.Item>
          </Descriptions>
          <Space wrap size={4} style={{ marginBottom: 8 }}>
            {(impact.middlewares ?? []).map((m) => (
              <span
                key={`${m.type}:${m.addr}`}
                onClick={() => {
                  navigate(
                    `/diagnosis?mwType=${encodeURIComponent(m.type)}&addr=${encodeURIComponent(m.addr)}&env=PRO`,
                  );
                }}
                style={{ cursor: 'pointer' }}
              >
                <StatusPill tone={m.isTarget ? 'warn' : 'neutral'}>
                  {m.type}:{m.addr}（{m.depApps}）
                </StatusPill>
              </span>
            ))}
          </Space>
        </>
      )}
      {impact.note && <Alert type="info" showIcon message={impact.note} style={{ marginTop: 8 }} />}
    </Card>
  );
}

// 日志证据卡：命中文件/行数/错误数 + 错误类型 TopN + 关键问题
function LogEvidenceCard({ step }: { step: Step }) {
  const r = (step.result ?? {}) as Record<string, any>;
  if (step.status !== 'ok') return null;
  const stats = [
    { label: '命中文件', value: Number(r.files ?? 0) },
    { label: '总行数', value: Number(r.lines ?? 0) },
    { label: '错误数', value: Number(r.errors ?? 0) },
    { label: '警告数', value: Number(r.warnings ?? 0) },
  ];
  const types: any[] = Array.isArray(r.topTypes) ? r.topTypes : [];
  const issues: any[] = Array.isArray(r.keyIssues) ? r.keyIssues : [];
  const fmt = (v: number) => v.toLocaleString();

  return (
    <Card
      size="small"
      style={{ marginTop: 8 }}
      title={
        <Space>
          <StatusPill tone="warn">日志证据</StatusPill>
          <span className="mono">{r.path || '-'}</span>
        </Space>
      }
      extra={r.taskId ? <span className="mono">task {r.taskId}</span> : null}
    >
      <Row gutter={8}>
        {stats.map((s) => (
          <Col span={6} key={s.label}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {s.label}
            </Typography.Text>
            <div style={{ fontSize: 20, fontWeight: 600 }}>{fmt(s.value)}</div>
          </Col>
        ))}
      </Row>
      {(types.length > 0 || issues.length > 0) && (
        <Row gutter={8} style={{ marginTop: 8 }}>
          <Col span={12}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              错误类型 Top{types.length}
            </Typography.Text>
            <Table
              size="small"
              rowKey="name"
              pagination={false}
              dataSource={types}
              locale={{ emptyText: '未解析到错误类型' }}
              columns={[
                { title: '类型', dataIndex: 'name', ellipsis: true },
                { title: '次数', dataIndex: 'count', width: 80, render: (v: number) => fmt(v) },
              ]}
            />
          </Col>
          <Col span={12}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              关键问题
            </Typography.Text>
            <Table
              size="small"
              rowKey="name"
              pagination={false}
              dataSource={issues}
              locale={{ emptyText: '未解析到关键问题' }}
              columns={[
                { title: '问题', dataIndex: 'name', ellipsis: true },
                { title: '次数', dataIndex: 'count', width: 80, render: (v: number) => fmt(v) },
                { title: '严重程度', dataIndex: 'severity', width: 90 },
              ]}
            />
          </Col>
        </Row>
      )}
      {r.note && (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {r.note}
        </Typography.Text>
      )}
    </Card>
  );
}

// 问题单卡：五步流程第 2 步「确认与定界」的产出。
function TicketCard({ ticket }: { ticket: DiagnosisSession['ticket'] }) {
  if (!ticket) return null;
  const e = ticket.entity ?? {};
  const entityText =
    [e.appId, e.middlewareType && `${e.middlewareType}:${e.addr}`, e.hostName, e.ip]
      .filter(Boolean)
      .join(' · ') || '-';
  const confTone: PillTone =
    ticket.confidence === 'confirmed' ? 'ok' : ticket.confidence === 'degraded' ? 'warn' : 'neutral';
  return (
    <Card
      size="small"
      style={{ marginBottom: 12 }}
      title={
        <Space>
          <StatusPill tone="info">问题单</StatusPill>
          <StatusPill tone={SEVERITY_TONE[ticket.severity] ?? 'neutral'} dot>
            {ticket.severity}
          </StatusPill>
          <StatusPill tone={confTone}>数据可信度 {ticket.confidence}</StatusPill>
        </Space>
      }
      extra={
        ticket.window?.minutes ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            观测窗口 {ticket.window.minutes} 分钟
          </Typography.Text>
        ) : null
      }
    >
      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="问题实体" span={2}>
          <span className="mono">{entityText}</span>
        </Descriptions.Item>
        <Descriptions.Item label="应用">{e.appName || e.appId || '-'}</Descriptions.Item>
        <Descriptions.Item label="归属人">{e.owner || '-'}</Descriptions.Item>
        <Descriptions.Item label="节点">{e.node || '-'}</Descriptions.Item>
        <Descriptions.Item label="信号来源">
          <Space wrap size={4}>
            {(ticket.signals ?? []).map((s) => (
              <StatusPill key={s} tone="neutral">
                {s}
              </StatusPill>
            ))}
          </Space>
        </Descriptions.Item>
        <Descriptions.Item label="影响面" span={2}>
          主机 {ticket.impact?.hosts ?? 0} · 应用 {ticket.impact?.apps ?? 0} · 中间件{' '}
          {ticket.impact?.middlewares ?? 0}
          {ticket.impact?.hostList?.length ? (
            <Typography.Text type="secondary" style={{ marginLeft: 8 }} className="mono">
              {ticket.impact.hostList.slice(0, 5).join(', ')}
              {ticket.impact.hostList.length > 5 ? ' …' : ''}
            </Typography.Text>
          ) : null}
        </Descriptions.Item>
      </Descriptions>
      {ticket.reason && (
        <Alert
          type={ticket.confidence === 'confirmed' ? 'success' : 'warning'}
          showIcon
          style={{ marginTop: 8 }}
          message={ticket.reason}
        />
      )}
    </Card>
  );
}

// 数据源验真卡：把每个 MCP 数据源自检结果摊开，避免「采集故障当业务故障」。
function SourceHealthCard({ health }: { health?: DiagnosisSession['sourceHealth'] }) {
  if (!health?.length) return null;
  return (
    <Card size="small" style={{ marginBottom: 12 }} title="数据源验真">
      <Table
        size="small"
        rowKey="source"
        pagination={false}
        dataSource={health}
        columns={[
          { title: '数据源', dataIndex: 'source', width: 110 },
          {
            title: '状态',
            dataIndex: 'status',
            width: 120,
            render: (v: string) => (
              <StatusPill tone={HEALTH_TONE[v] ?? 'neutral'} dot>
                {v}
              </StatusPill>
            ),
          },
          {
            title: '检查项',
            dataIndex: 'checks',
            render: (v: string[]) => (v?.length ? v.join(' / ') : '-'),
          },
          { title: '说明', dataIndex: 'detail', ellipsis: true, render: (v?: string) => v || '-' },
        ]}
      />
    </Card>
  );
}

export default function Diagnosis() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const [form] = Form.useForm();
  const [session, setSession] = useState<DiagnosisSession | null>(null);
  const [steps, setSteps] = useState<Step[]>([]);
  const [live, setLive] = useState(false);
  const cancelRef = useRef<null | (() => void)>(null);
  const [previewing, setPreviewing] = useState<Impact | null>(null);
  // 根因准确率人工标注（T10）
  const [fbAccurate, setFbAccurate] = useState<boolean | undefined>();
  const [fbRootCause, setFbRootCause] = useState('');
  const [fbNote, setFbNote] = useState('');

  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  // URL 参数预填（从运维关系拓扑等页面跳转过来时）
  useEffect(() => {
    const appId = searchParams.get('appId');
    const mwType = searchParams.get('mwType');
    const addr = searchParams.get('addr');
    const symptom = searchParams.get('symptom');
    if (appId || mwType) {
      const patch: Record<string, string> = {};
      if (appId) patch.appId = appId;
      if (mwType) patch.middlewareType = mwType;
      if (addr) patch.addr = addr;
      if (symptom) patch.symptom = symptom;
      const env = searchParams.get('env');
      if (env) patch.env = env;
      form.setFieldsValue(patch);
    }
  }, [searchParams, form]);

  useEffect(() => () => cancelRef.current?.(), []);

  // 切换会话时重置标注编辑态
  useEffect(() => {
    setFbAccurate(undefined);
    setFbRootCause('');
    setFbNote('');
  }, [session?.id]);

  const apps = useQuery({ queryKey: ['apps-diag'], queryFn: () => api.topoApps() });
  const mwTargets = useQuery({
    queryKey: ['mw-targets-diag'],
    queryFn: () => api.opMwTargets({ env: 'PRO' }),
  });
  const sessions = useQuery({
    queryKey: ['diag-sessions'],
    queryFn: () => api.diagnoseSessions(20),
    refetchInterval: 8000,
  });

  // 当前排障目标相关的活跃告警（顶部横幅）
  const firingAlarms = useQuery({
    queryKey: ['diag-alarms-banner'],
    queryFn: () => api.alarmList({ state: 'firing', pageSize: 200 }),
    refetchInterval: 20000,
  });

  const start = useMutation({
    mutationFn: (v: any) =>
      api.createDiagnosis({
        tenantId: 'default',
        appId: v.appId || undefined,
        middlewareType: v.middlewareType || undefined,
        addr: v.addr || undefined,
        symptom: v.symptom || '未填写现象',
        env: v.env || 'PRO',
        date: v.date || undefined,
        viewMode: v.viewMode || 'folded',
        logPath: v.logPath || undefined,
      }),
    onSuccess: (s) => {
      setSession(s);
      setSteps([...s.steps]);
      setLive(true);
      cancelRef.current?.();
      cancelRef.current = streamDiagnosis(s.id, {
        onStep: (st) =>
          setSteps((prev) => {
            const idx = prev.findIndex((p) => p.no === st.no);
            if (idx >= 0) {
              const next = [...prev];
              next[idx] = st;
              return next;
            }
            return [...prev, st];
          }),
        onSession: (meta) => {
          setSession((prev) =>
            prev
              ? {
                  ...prev,
                  status: meta.status as DiagnosisSession['status'],
                  confidence: meta.confidence,
                  rootCause: meta.rootCause,
                }
              : prev,
          );
        },
        onDone: (final) => {
          setSession(final);
          setSteps(final.steps ?? []);
          setLive(false);
          qc.invalidateQueries({ queryKey: ['diag-sessions'] });
        },
        onError: (msg) => {
          message.warning(msg);
          setLive(false);
        },
      });
      message.success('会话已创建，证据链采集中');
      qc.invalidateQueries({ queryKey: ['diag-sessions'] });
      setPreviewing(null);
    },
    onError: (e: Error) => message.error(e.message),
  });

  // "从中间件反查预览"：不创建会话，只调 impact 接口看反向查询结果
  const previewMw = useMutation({
    mutationFn: async ({ mwType, addr }: { mwType: string; addr: string }) =>
      api.topoImpact({ mwType, addr, viewMode: 'folded' }),
    onSuccess: (im) => setPreviewing(im),
    onError: (e) => message.error((e as Error).message),
  });

  // 人工回填根因准确率（T10），同时刷新 KPI 准确率指标
  const feedback = useMutation({
    mutationFn: (v: { id: string; accurate: boolean; actualRootCause?: string; note?: string }) =>
      api.diagnoseFeedback(v.id, {
        accurate: v.accurate,
        actualRootCause: v.actualRootCause,
        note: v.note,
      }),
    onSuccess: (s) => {
      setSession(s);
      setFbAccurate(undefined);
      setFbRootCause('');
      setFbNote('');
      message.success('标注已保存，KPI 准确率随之更新');
      qc.invalidateQueries({ queryKey: ['diag-sessions'] });
      qc.invalidateQueries({ queryKey: ['kpi'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  // 中间件候选 Select 的 options：按类型分组，显示 addr + 依赖应用数
  const mwTargetOptions = useMemo(() => {
    const targets = mwTargets.data?.targets ?? [];
    const groups: Record<string, OpMwTarget[]> = {};
    for (const t of targets) {
      (groups[t.mwType] ||= []).push(t);
    }
    return Object.entries(groups).map(([type, list]) => ({
      label: type,
      options: list.map((t) => ({
        value: `${t.mwType}|${t.addr}`,
        label: `${t.addr}（${t.depApps} 应用）`,
      })),
    }));
  }, [mwTargets.data]);

  const openSession = async (id: string) => {
    cancelRef.current?.();
    setLive(false);
    try {
      const s = await api.diagnoseSession(id);
      setSession(s);
      setSteps(s.steps ?? []);
    } catch (e) {
      message.error((e as Error).message);
    }
  };

  const mwGroup = Form.useWatch('middlewareGroup', form);
  useEffect(() => {
    if (!mwGroup) return;
    const [mwType, addr] = mwGroup.split('|');
    form.setFieldsValue({ middlewareType: mwType, addr });
  }, [mwGroup, form]);

  // Hook 必须无条件调用且顺序稳定，否则切换表单会触发 React Hook 顺序异常
  const watchMwType = Form.useWatch('middlewareType', form);
  const watchAppId = Form.useWatch('appId', form);
  const watchAddr = Form.useWatch('addr', form);
  const formHasMw = !!watchMwType && !!watchAddr;

  // 当前表单 / 会话目标命中的活跃告警
  const relatedAlarms = useMemo(() => {
    const items = firingAlarms.data?.items ?? [];
    const appId = watchAppId || session?.appId || '';
    const addr = watchAddr || session?.addr || '';
    const host = addr.split(':')[0];
    if (!appId && !host) return items.slice(0, 3);
    return items.filter((a) => {
      const aHost = a.host || (a.instance || '').split(':')[0];
      if (host && (aHost === host || a.instance === addr || (a.instance || '').includes(host))) {
        return true;
      }
      if (appId && (a.labels?.appId === appId || a.labels?.app === appId || a.labels?.node === appId)) {
        return true;
      }
      return false;
    });
  }, [firingAlarms.data, session, watchAppId, watchAddr]);

  return (
    <>
      {relatedAlarms.length > 0 && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message={`当前目标存在 ${relatedAlarms.length} 条活跃告警`}
          description={
            <Space direction="vertical" size={2}>
              {relatedAlarms.slice(0, 3).map((a) => (
                <Space key={a.fingerprint} size={8} wrap>
                  <StatusPill tone={a.severity === 'critical' ? 'risk' : 'warn'} size="sm" dot>
                    {a.severity === 'critical' ? '严重' : '警告'}
                  </StatusPill>
                  <Typography.Text strong style={{ fontSize: 12 }}>
                    {a.alertName}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {a.instance || a.host || '-'}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {a.summary}
                  </Typography.Text>
                </Space>
              ))}
              {relatedAlarms.length > 3 && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  还有 {relatedAlarms.length - 3} 条，见告警中心
                </Typography.Text>
              )}
            </Space>
          }
          action={
            <Button size="small" onClick={() => navigate('/alert-center')}>
              去告警中心
            </Button>
          }
        />
      )}
      <Row gutter={12}>
      <Col xs={24} xl={8}>
        <Card size="small" title="发起排障" style={{ marginBottom: 12 }}>
          <Form
            form={form}
            layout="vertical"
            initialValues={{ env: 'PRO', viewMode: 'folded' }}
            onFinish={(v) => start.mutate(v)}
          >
            <Form.Item name="appId" label="应用 appId（与中间件二选一）">
              <Select
                showSearch
                allowClear
                placeholder="例如 area"
                optionFilterProp="label"
                options={(apps.data?.apps ?? []).map((a) => ({
                  value: a.appId,
                  label: `${a.appName || a.appId}（${a.appId}）`,
                }))}
              />
            </Form.Item>

            <Divider plain style={{ marginTop: -4 }}>或者 — 直接选中间件反查</Divider>

            <Form.Item name="middlewareGroup" label="中间件候选（按类型分组）">
              <Select
                showSearch
                allowClear
                placeholder="选择中间件 → 自动填类型和地址"
                optionFilterProp="label"
                options={mwTargetOptions}
                loading={mwTargets.isLoading}
              />
            </Form.Item>
            <Space style={{ width: '100%' }}>
              <Form.Item name="middlewareType" label="中间件类型" style={{ marginBottom: 8 }}>
                <Input placeholder="mysql / redis ..." />
              </Form.Item>
              <Form.Item name="addr" label="中间件地址" style={{ marginBottom: 8 }}>
                <Input placeholder="host:port" />
              </Form.Item>
            </Space>

            <Form.Item
              name="logPath"
              label="日志路径（可选，留空按 LOG_PATH_TEMPLATE 推导）"
              style={{ marginBottom: 8 }}
            >
              <Input placeholder="/tmp/aiops-logs/10.0.0.1.log" />
            </Form.Item>

            {formHasMw && !watchAppId && (
              <Button
                size="small"
                type="dashed"
                block
                loading={previewMw.isPending}
                icon={<SearchOutlined />}
                onClick={() => {
                  const v = form.getFieldsValue();
                  previewMw.mutate({ mwType: v.middlewareType, addr: v.addr });
                }}
                style={{ marginBottom: 8 }}
              >
                预览该中间件影响的应用
              </Button>
            )}

            {previewing && (
              <Alert
                type="info"
                showIcon
                message={`反向查询：该中间件被 ${previewing.totalApps} 个应用依赖`}
                description={
                  <Button
                    size="small"
                    type="link"
                    onClick={() => {
                      navigate(
                        `/op-topology?expandDepNode=0&middlewareType=${encodeURIComponent(previewing.target.split(':')[0])}`,
                      );
                    }}
                  >
                    查看完整拓扑
                  </Button>
                }
                style={{ marginBottom: 8 }}
              />
            )}

            <Form.Item name="symptom" label="现象描述">
              <Input.TextArea rows={2} placeholder="例如：下单接口 5xx 升高，P99 超 3s" />
            </Form.Item>
            <Form.Item name="env" label="环境">
              <Input />
            </Form.Item>
            <Form.Item name="viewMode" label="拓扑口径">
              <Segmented
                options={[
                  { label: '折叠', value: 'folded' },
                  { label: '展开', value: 'expanded' },
                ]}
              />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={start.isPending}>
              开始排障
            </Button>
          </Form>
        </Card>

        <Card size="small" title="最近会话">
          <List
            size="small"
            loading={sessions.isLoading}
            dataSource={sessions.data?.sessions ?? []}
            locale={{ emptyText: '暂无会话' }}
            renderItem={(s: DiagnosisSession) => (
              <List.Item
                style={{ cursor: 'pointer' }}
                onClick={() => openSession(s.id)}
                actions={[
                  <StatusPill key="st" tone={STATUS_TONE[s.status] ?? 'neutral'} dot>
                    {s.status}
                  </StatusPill>,
                ]}
              >
                <List.Item.Meta
                  title={s.appId || `${s.middlewareType}:${s.addr}`}
                  description={
                    <span className="mono">
                      {new Date(s.createdAt).toLocaleString()} · {s.symptom}
                    </span>
                  }
                />
              </List.Item>
            )}
          />
        </Card>
      </Col>

      <Col xs={24} xl={16}>
        {!session && (
          <Card size="small">
            <Empty description="发起一次排障，或从左侧选择历史会话" />
          </Card>
        )}
        {session && (
          <>
            <SourceHealthCard health={session.sourceHealth} />
            <TicketCard ticket={session.ticket} />
            <Card
              size="small"
              title={
                <Space>
                  <span>会话 {session.id}</span>
                  <Badge status={live ? 'processing' : 'default'} text={live ? '采集中' : '已结束'} />
                  <StatusPill tone={STATUS_TONE[session.status] ?? 'neutral'} dot>{session.status}</StatusPill>
                </Space>
              }
              extra={
                <Space>
                  <span>置信度</span>
                  <Progress
                    type="circle"
                    size={44}
                    percent={Math.round(session.confidence * 100)}
                    status={session.confidence >= 0.6 ? 'success' : 'normal'}
                  />
                </Space>
              }
              style={{ marginBottom: 12 }}
            >
              <Space direction="vertical" style={{ width: '100%' }}>
                <div>
                  <Typography.Text type="secondary">根因结论：</Typography.Text>
                  <Typography.Text strong>{session.rootCause || '推断中…'}</Typography.Text>
                </div>
                {session.candidates?.length && (
                  <Table
                    size="small"
                    rowKey="title"
                    pagination={false}
                    dataSource={session.candidates}
                    columns={[
                      { title: '根因候选', dataIndex: 'title' },
                      { title: '依据', dataIndex: 'reason', ellipsis: true },
                      {
                        title: '置信度',
                        dataIndex: 'confidence',
                        width: 100,
                        render: (v: number) => `${Math.round(v * 100)}%`,
                      },
                      { title: '来源', dataIndex: 'source', width: 100 },
                    ]}
                  />
                )}
              </Space>
            </Card>

            <Card
              size="small"
              style={{ marginBottom: 12 }}
              title={
                <Space>
                  <span>根因准确率回填</span>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    人工标注，用于 KPI 准确率统计
                  </Typography.Text>
                </Space>
              }
              extra={
                session.feedback ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    最近标注 {new Date(session.feedback.at).toLocaleString()}
                  </Typography.Text>
                ) : null
              }
            >
              {session.feedback && (
                <Alert
                  type={session.feedback.accurate ? 'success' : 'warning'}
                  showIcon
                  style={{ marginBottom: 8 }}
                  message={session.feedback.accurate ? '已标注：平台根因结论准确' : '已标注：平台根因结论不准确'}
                  description={
                    <Space direction="vertical" size={2}>
                      {session.feedback.actualRootCause && (
                        <span>真实根因：{session.feedback.actualRootCause}</span>
                      )}
                      {session.feedback.note && <span>备注：{session.feedback.note}</span>}
                      {session.feedback.operator && <span>标注人：{session.feedback.operator}</span>}
                    </Space>
                  }
                />
              )}
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
                <Space wrap>
                  <Typography.Text type="secondary">本次结论：</Typography.Text>
                  <Typography.Text strong>{session.rootCause || '—'}</Typography.Text>
                </Space>
                <Space wrap>
                  <Button
                    size="small"
                    type={fbAccurate === true ? 'primary' : 'default'}
                    onClick={() => setFbAccurate(true)}
                  >
                    结论准确
                  </Button>
                  <Button
                    size="small"
                    danger
                    type={fbAccurate === false ? 'primary' : 'default'}
                    onClick={() => setFbAccurate(false)}
                  >
                    结论不准确
                  </Button>
                  <Button
                    size="small"
                    type="primary"
                    disabled={fbAccurate === undefined}
                    loading={feedback.isPending}
                    onClick={() =>
                      feedback.mutate({
                        id: session.id,
                        accurate: fbAccurate as boolean,
                        actualRootCause: fbAccurate ? undefined : fbRootCause || undefined,
                        note: fbNote || undefined,
                      })
                    }
                  >
                    提交标注
                  </Button>
                </Space>
                {fbAccurate === false && (
                  <Input
                    placeholder="真实根因（选填）"
                    value={fbRootCause}
                    onChange={(e) => setFbRootCause(e.target.value)}
                  />
                )}
                {fbAccurate !== undefined && (
                  <Input.TextArea
                    rows={2}
                    placeholder="备注（选填）"
                    value={fbNote}
                    onChange={(e) => setFbNote(e.target.value)}
                  />
                )}
              </Space>
            </Card>

            <Card size="small" title={`证据链（${steps.length} 步）`}>
              {steps.length === 0 && <Empty description="暂无步骤" />}
              <div className="step-timeline">
                <Timeline
                  items={steps.map((s) => ({
                    color: STEP_COLOR[s.status] ?? 'blue',
                    children: (
                      <div>
                        <Space wrap>
                          <StatusPill tone={STEP_TONE[s.status] ?? 'info'} dot>{s.status}</StatusPill>
                          <Typography.Text strong>{s.title}</Typography.Text>
                          <StatusPill tone="neutral">{s.source}</StatusPill>
                          <span className="mono">{s.tool}</span>
                          <span className="mono">{s.latencyMs}ms</span>
                        </Space>
                        {s.error && (
                          <Alert style={{ marginTop: 6 }} type="error" showIcon message={s.error} />
                        )}
                        {s.tool === 'impact' && session.impact && (
                          <ImpactCard impact={session.impact} level={session.impact.level} />
                        )}
                        {s.tool === 'log_analyzer' && <LogEvidenceCard step={s} />}
                        {(s.args || s.result) && (
                          <pre className="mono" style={{ marginTop: 6, maxHeight: 180, overflow: 'auto' }}>
                            {JSON.stringify({ args: s.args, result: s.result }, null, 2)}
                          </pre>
                        )}
                      </div>
                    ),
                  }))}
                />
              </div>
            </Card>
          </>
        )}
      </Col>
    </Row>
    </>
  );
}
