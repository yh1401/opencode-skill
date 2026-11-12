import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Empty,
  InputNumber,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tooltip,
  Typography,
} from 'antd';
import { BugOutlined, PlayCircleOutlined, SyncOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type { AuditFinding, AuditRun } from '../types';

const CHECK_META: Record<string, { label: string; tone: PillTone }> = {
  AUDIT_C1: { label: 'C1 存在性对账', tone: 'info' },
  AUDIT_C2: { label: 'C2 状态一致性', tone: 'info' },
  AUDIT_C3: { label: 'C3 反向孤儿', tone: 'info' },
  AUDIT_C4: { label: 'C4 指标佐证', tone: 'warn' },
};

const SEV_TONE: Record<string, PillTone> = { P0: 'risk', P1: 'warn', P2: 'warn', P3: 'neutral' };
const VERDICT_TONE: Record<string, PillTone> = {
  RISK: 'risk',
  NOT_TRIGGERED: 'ok',
  NO_DATA: 'neutral',
  NOT_APPLICABLE: 'neutral',
};
const VERDICT_LABEL: Record<string, string> = {
  RISK: '风险',
  NOT_TRIGGERED: '正常',
  NO_DATA: '无数据',
  NOT_APPLICABLE: '不适用',
};

export default function Audit() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const [date, setDate] = useState<string | undefined>();
  const [env, setEnv] = useState<string>('PRO');
  const [maxHosts, setMaxHosts] = useState<number>(20);
  const [selectedRun, setSelectedRun] = useState<AuditRun | null>(null);

  const dates = useQuery({ queryKey: ['audit-dates'], queryFn: api.topoDates });
  const runs = useQuery({ queryKey: ['audit-runs'], queryFn: () => api.auditRuns(20) });

  const run = useMutation({
    mutationFn: () => api.auditRun({ date, env, maxHosts }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ['audit-runs'] });
      message.success(r.message ?? '稽核完成');
    },
    onError: (e: Error) => message.error(e.message),
  });

  return (
    <Row gutter={[12, 12]}>
      {/* 触发栏 */}
      <Col span={24}>
        <Card size="small" title="资源稽核（CMDB 账实对账）" extra={
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            数据源：拓扑快照（本地）+ CMDB 上游服务 + Prometheus 上游服务
          </Typography.Text>
        }>
          <Space wrap>
            <span>快照：</span>
            <Select
              placeholder="最新"
              style={{ width: 150 }}
              allowClear
              value={date}
              onChange={setDate}
              options={(dates.data?.dates ?? []).map((d) => ({ value: d, label: d }))}
            />
            <span>环境：</span>
            <Segmented options={['PRO', 'SIT', 'DEV']} value={env} onChange={(v) => setEnv(v as string)} />
            <span>对账上限：</span>
            <InputNumber min={1} max={100} value={maxHosts} onChange={(v) => setMaxHosts(v ?? 20)} />
            <span style={{ color: '#999', fontSize: 12 }}>主机</span>
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              loading={run.isPending}
              onClick={() => run.mutate()}
            >
              执行稽核
            </Button>
          </Space>
          <div style={{ marginTop: 8, fontSize: 12, color: '#8c8c8c' }}>
            C1 存在性（拓扑中间件 host ↔ CMDB 资产） · C2 状态一致性（state=0 在线） · C3 反向孤儿（CMDB 在线未被拓扑引用） · C4 指标佐证（Prometheus up 心跳）
          </div>
        </Card>
      </Col>

      {/* 批次列表 */}
      <Col span={24}>
        <Card size="small" title={`稽核批次（${runs.data?.total ?? 0}）`}>
          {runs.isLoading && <Spin />}
          {!runs.isLoading && (runs.data?.runs?.length ?? 0) === 0 && (
            <Empty description="暂无稽核批次，点击「执行稽核」开始" />
          )}
          {(runs.data?.runs?.length ?? 0) > 0 && (
            <Table
              size="small"
              rowKey="id"
              dataSource={runs.data?.runs}
              pagination={{ pageSize: 6, size: 'small' }}
              columns={[
                {
                  title: '批次',
                  width: 100,
                  render: (_, r) => <Typography.Text code>{r.id.slice(0, 8)}</Typography.Text>,
                },
                { title: '快照', dataIndex: 'date', width: 110 },
                { title: '环境', dataIndex: 'env', width: 70 },
                {
                  title: '状态',
                  width: 90,
                  dataIndex: 'status',
                  render: (s: string) =>
                    s === 'succeeded' ? (
                      <StatusPill tone="ok" size="sm" dot>完成</StatusPill>
                    ) : s === 'failed' ? (
                      <StatusPill tone="risk" size="sm" dot>失败</StatusPill>
                    ) : (
                      <StatusPill tone="info" size="sm" dot><SyncOutlined spin /> 运行中</StatusPill>
                    ),
                },
                {
                  title: '统计',
                  render: (_, r) => (
                    <Space size={[4, 4]} wrap>
                      <StatusPill tone="neutral" size="sm">{r.stats.items} 项</StatusPill>
                      <StatusPill tone={r.stats.risk > 0 ? 'risk' : 'neutral'} size="sm">风险 {r.stats.risk}</StatusPill>
                      <StatusPill tone="ok" size="sm">正常 {r.stats.notTriggered}</StatusPill>
                      {r.stats.noData > 0 && <StatusPill tone="neutral" size="sm">无数据 {r.stats.noData}</StatusPill>}
                    </Space>
                  ),
                },
                {
                  title: '耗时',
                  width: 90,
                  render: (_, r) =>
                    `${Math.max(1, Math.round((new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime()) / 100) / 10)}s`,
                },
                {
                  title: '时间',
                  width: 150,
                  dataIndex: 'startedAt',
                  render: (v: string) => new Date(v).toLocaleString(),
                },
                {
                  title: '操作',
                  width: 80,
                  render: (_, r) => (
                    <Button size="small" type="link" onClick={() => setSelectedRun(r)}>
                      详情
                    </Button>
                  ),
                },
              ]}
            />
          )}
        </Card>
      </Col>

      {/* 批次详情 Drawer */}
      {selectedRun && <RunDrawer run={selectedRun} onClose={() => setSelectedRun(null)} />}
    </Row>
  );
}

function RunDrawer({ run: selectedRun, onClose }: { run: AuditRun; onClose: () => void }) {
  const navigate = useNavigate();
  const findings = useQuery({
    queryKey: ['audit-findings', selectedRun.id],
    queryFn: () => api.auditFindings(selectedRun.id),
  });
  // 风险项一键转排障：中间件视角需要 middlewareType + addr，缺少类型（如 C3 孤儿）时不可跳转
  const diagTarget = (f: AuditFinding) => {
    const mwType = typeof f.evidence?.middlewareType === 'string' ? f.evidence.middlewareType : '';
    return mwType ? { mwType, addr: f.objectKey } : null;
  };
  const openDiagnosis = (f: AuditFinding) => {
    const t = diagTarget(f);
    if (!t) return;
    const params = new URLSearchParams({
      mwType: t.mwType,
      addr: t.addr,
      env: selectedRun.env,
      symptom: `稽核发现 ${CHECK_META[f.checkCode]?.label ?? f.checkCode} 风险：${f.detail}`,
    });
    navigate(`/diagnosis?${params.toString()}`);
  };
  const risks = (findings.data?.findings ?? []).filter((f) => f.verdict === 'RISK');
  return (
    <Drawer
      width={760}
      open
      onClose={onClose}
      title={`稽核批次 ${selectedRun.id.slice(0, 8)}（${selectedRun.date} · ${selectedRun.env}）`}
    >
      <Row gutter={12} style={{ marginBottom: 12 }}>
        <Col span={5}><Card size="small"><Statistic title="总项" value={selectedRun.stats.items} /></Card></Col>
        <Col span={5}><Card size="small"><Statistic title="风险" value={selectedRun.stats.risk} valueStyle={{ color: selectedRun.stats.risk > 0 ? '#cf1322' : undefined }} /></Card></Col>
        <Col span={5}><Card size="small"><Statistic title="正常" value={selectedRun.stats.notTriggered} valueStyle={{ color: '#3f8600' }} /></Card></Col>
        <Col span={5}><Card size="small"><Statistic title="无数据" value={selectedRun.stats.noData} /></Card></Col>
        <Col span={4}><Card size="small"><Statistic title="不适用" value={selectedRun.stats.notApplicable} /></Card></Col>
      </Row>

      {selectedRun.message && (
        <Alert
          type={selectedRun.stats.risk > 0 ? 'warning' : 'success'}
          showIcon
          message={selectedRun.message}
          style={{ marginBottom: 12 }}
        />
      )}

      {findings.isLoading && <Spin style={{ display: 'block', margin: '24px auto' }} />}
      {!findings.isLoading && (findings.data?.total ?? 0) === 0 && <Empty description="该批次无发现" />}

      {risks.length > 0 && (
        <>
          <Typography.Title level={5} style={{ marginTop: 0 }}>风险项（需处置）</Typography.Title>
          {risks.map((f: AuditFinding) => (
            <Card key={f.id} size="small" style={{ marginBottom: 8 }}>
              <Space wrap>
                <StatusPill tone={CHECK_META[f.checkCode]?.tone}>{CHECK_META[f.checkCode]?.label ?? f.checkCode}</StatusPill>
                <StatusPill tone={SEV_TONE[f.severity]} dot>{f.severity}</StatusPill>
                <Typography.Text strong>
                  {f.objectType === 'middleware' ? '中间件' : '服务器'}：{f.objectKey}
                </Typography.Text>
                {diagTarget(f) ? (
                  <Button size="small" type="link" icon={<BugOutlined />} onClick={() => openDiagnosis(f)}>
                    开排障
                  </Button>
                ) : (
                  <Tooltip title="该对象未出现在拓扑依赖关系中，无法确定中间件类型，请在「排障工作台」手动指定目标">
                    <Button size="small" type="link" icon={<BugOutlined />} disabled>
                      开排障
                    </Button>
                  </Tooltip>
                )}
              </Space>
              <div style={{ marginTop: 6 }}>{f.detail}</div>
              {f.evidence && (
                <Descriptions size="small" column={3} style={{ marginTop: 8 }} bordered>
                  {Object.entries(f.evidence).slice(0, 6).map(([k, v]) => (
                    <Descriptions.Item key={k} label={k}>
                      <span style={{ fontSize: 12 }}>{typeof v === 'object' ? JSON.stringify(v) : String(v)}</span>
                    </Descriptions.Item>
                  ))}
                </Descriptions>
              )}
            </Card>
          ))}
        </>
      )}

      {(findings.data?.findings ?? []).some((f) => f.verdict !== 'RISK') && (
        <>
          <Typography.Title level={5}>正常 / 无数据项</Typography.Title>
          <Table
            size="small"
            rowKey="id"
            pagination={{ pageSize: 8, size: 'small' }}
            dataSource={(findings.data?.findings ?? []).filter((f) => f.verdict !== 'RISK')}
            columns={[
              {
                title: '检查',
                width: 130,
                render: (_, f) => <StatusPill tone={CHECK_META[f.checkCode]?.tone} size="sm">{CHECK_META[f.checkCode]?.label ?? f.checkCode}</StatusPill>,
              },
              { title: '对象', ellipsis: true, render: (_, f) => `${f.objectType}:${f.objectKey}` },
              {
                title: '结论',
                width: 90,
                render: (_, f) => <StatusPill tone={VERDICT_TONE[f.verdict]} size="sm" dot>{VERDICT_LABEL[f.verdict]}</StatusPill>,
              },
              { title: '说明', ellipsis: true, dataIndex: 'detail' },
            ]}
          />
        </>
      )}
    </Drawer>
  );
}
