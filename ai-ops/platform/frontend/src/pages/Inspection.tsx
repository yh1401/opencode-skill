import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  App as AntdApp,
  Button,
  Card,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Space,
  Table,
  Typography,
} from 'antd';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type { Finding, InspectionRun, InspectionTask } from '../types';

const STATUS_TONE: Record<string, PillTone> = {
  pending: 'neutral',
  running: 'info',
  succeeded: 'ok',
  failed: 'risk',
  partial: 'warn',
};

const VERDICT_TONE: Record<string, PillTone> = {
  RISK: 'risk',
  NOT_TRIGGERED: 'neutral',
  NO_DATA: 'warn',
  NOT_APPLICABLE: 'info',
};

export default function Inspection() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const [taskModal, setTaskModal] = useState(false);
  const [activeRun, setActiveRun] = useState<InspectionRun | null>(null);
  const [report, setReport] = useState<{ runId: string; content: string } | null>(null);
  const [form] = Form.useForm();

  const tasks = useQuery({ queryKey: ['tasks'], queryFn: api.inspectTasks });
  const runs = useQuery({
    queryKey: ['runs'],
    queryFn: () => api.inspectRuns(50),
    refetchInterval: (q) =>
      (q.state.data?.runs ?? []).some((r) => r.status === 'running' || r.status === 'pending')
        ? 2000
        : false,
  });
  const findings = useQuery({
    queryKey: ['findings', activeRun?.id],
    queryFn: () => api.findings(activeRun!.id),
    enabled: !!activeRun,
  });

  const runTask = useMutation({
    mutationFn: (id: string) => api.runTask(id),
    onSuccess: (run) => {
      message.success(`已触发：${run.taskName}`);
      qc.invalidateQueries({ queryKey: ['runs'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const createTask = useMutation({
    mutationFn: (v: any) =>
      api.createTask({
        name: v.name,
        scene: 'topo',
        cron: v.cron ? v.cron.trim() : '',
        scope: {
          env: v.env || 'PRO',
          date: v.date || undefined,
          appIds: v.appIds ? v.appIds.split(',').map((s: string) => s.trim()).filter(Boolean) : [],
          mwTypes: v.mwTypes
            ? v.mwTypes.split(',').map((s: string) => s.trim()).filter(Boolean)
            : [],
        },
      }),
    onSuccess: () => {
      message.success('任务已创建');
      setTaskModal(false);
      form.resetFields();
      qc.invalidateQueries({ queryKey: ['tasks'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const deleteTask = useMutation({
    mutationFn: (id: string) => api.deleteTask(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
    onError: (e: Error) => message.error(e.message),
  });

  const openReport = async (run: InspectionRun) => {
    try {
      const content = await api.report(run.id);
      setReport({ runId: run.id, content });
    } catch (e) {
      message.error((e as Error).message);
    }
  };

  return (
    <>
      <Card
        size="small"
        title="巡检任务"
        extra={
          <Space>
            <Button onClick={() => setTaskModal(true)}>新建任务</Button>
            <Button
              type="primary"
              loading={runTask.isPending}
              onClick={() => {
                const t = tasks.data?.tasks[0];
                if (t) runTask.mutate(t.id);
              }}
            >
              运行默认任务
            </Button>
          </Space>
        }
        style={{ marginBottom: 12 }}
      >
        <Table
          size="small"
          rowKey="id"
          loading={tasks.isLoading}
          scroll={{ x: 'max-content' }}
          dataSource={tasks.data?.tasks ?? []}
          pagination={false}
          columns={[
            { title: '任务', dataIndex: 'name' },
            {
              title: '范围',
              render: (_, r: InspectionTask) => (
                <span className="mono">
                  env={r.scope.env || '-'} date={r.scope.date || 'latest'} apps=
                  {r.scope.appIds?.length || 0} mwTypes={r.scope.mwTypes?.length || 0}
                </span>
              ),
            },
            { title: '规则集', dataIndex: 'ruleSetVersion', width: 160 },
            {
              title: 'Cron',
              width: 140,
              render: (_, r: InspectionTask) =>
                r.cron ? <span className="mono">{r.cron}</span> : <Typography.Text type="secondary">手动</Typography.Text>,
            },
            {
              title: '操作',
              width: 200,
              render: (_, r: InspectionTask) => (
                <Space>
                  <Button size="small" type="link" onClick={() => runTask.mutate(r.id)}>
                    运行
                  </Button>
                  <Button
                    size="small"
                    type="link"
                    danger
                    onClick={() => deleteTask.mutate(r.id)}
                  >
                    删除
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Card size="small" title="巡检批次">
        <Table
          size="small"
          rowKey="id"
          loading={runs.isLoading}
          dataSource={runs.data?.runs ?? []}
          pagination={{ pageSize: 10, size: 'small' }}
          columns={[
            { title: '任务', dataIndex: 'taskName', ellipsis: true },
            { title: '触发', dataIndex: 'trigger', width: 80 },
            { title: '快照', dataIndex: 'topoSyncDate', width: 110 },
            {
              title: '状态',
              dataIndex: 'status',
              width: 100,
              render: (v: string) => (
                <StatusPill tone={STATUS_TONE[v] ?? 'neutral'} size="sm" dot>
                  {v}
                </StatusPill>
              ),
            },
            {
              title: '四态统计',
              render: (_, r: InspectionRun) => (
                <Space size={4} wrap>
                  <StatusPill tone="neutral" size="sm">项 {r.stats.items}</StatusPill>
                  <StatusPill tone="risk" size="sm">风险 {r.stats.risk}</StatusPill>
                  <StatusPill tone="warn" size="sm">无数据 {r.stats.noData}</StatusPill>
                  <StatusPill tone="info" size="sm">不适用 {r.stats.notApplicable}</StatusPill>
                  <StatusPill tone="neutral" size="sm">未触发 {r.stats.notTriggered}</StatusPill>
                </Space>
              ),
            },
            {
              title: '开始时间',
              dataIndex: 'startedAt',
              width: 180,
              render: (v: string) => new Date(v).toLocaleString(),
            },
            {
              title: '操作',
              width: 180,
              render: (_, r: InspectionRun) => (
                <Space>
                  <Button size="small" type="link" onClick={() => setActiveRun(r)}>
                    发现
                  </Button>
                  <Button size="small" type="link" onClick={() => openReport(r)}>
                    报告
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={taskModal}
        title="新建巡检任务"
        onCancel={() => setTaskModal(false)}
        onOk={() => form.submit()}
        confirmLoading={createTask.isPending}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(v) => createTask.mutate(v)}
          initialValues={{ env: 'PRO' }}
        >
          <Form.Item name="name" label="任务名" rules={[{ required: true, message: '请输入任务名' }]}>
            <Input placeholder="例如：核心交易应用每日巡检" />
          </Form.Item>
          <Form.Item name="env" label="环境">
            <Input placeholder="PRO / UAT / DEV" />
          </Form.Item>
          <Form.Item name="date" label="快照日期（留空 = 最新）">
            <Input placeholder="2026-10-08" />
          </Form.Item>
          <Form.Item name="appIds" label="应用 appId（逗号分隔，留空 = 全量）">
            <Input placeholder="area,order-center" />
          </Form.Item>
          <Form.Item name="mwTypes" label="中间件类型（逗号分隔，留空 = 全部）">
            <Input placeholder="mysql,redis,rocketmq" />
          </Form.Item>
          <Form.Item
            name="cron"
            label="定时调度 Cron（5 段：分 时 日 月 周，留空 = 仅手动）"
            rules={[
              {
                pattern: /^\s*$|^\s*(\S+\s+){4}\S+\s*$/,
                message: '请输入 5 段 cron，例如 0 2 * * *',
              },
            ]}
          >
            <Input placeholder="0 2 * * *（每天 02:00）" />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        open={!!activeRun}
        width={980}
        title={`发现 · ${activeRun?.taskName ?? ''} · ${activeRun?.id ?? ''}`}
        onClose={() => setActiveRun(null)}
      >
        {activeRun && (
          <Descriptions size="small" column={2} style={{ marginBottom: 12 }}>
            <Descriptions.Item label="状态">
              <StatusPill tone={STATUS_TONE[activeRun.status] ?? 'neutral'} dot>
                {activeRun.status}
              </StatusPill>
            </Descriptions.Item>
            <Descriptions.Item label="快照">{activeRun.topoSyncDate}</Descriptions.Item>
            <Descriptions.Item label="明细" span={2}>
              {activeRun.message || '-'}
            </Descriptions.Item>
          </Descriptions>
        )}
        <Table
          size="small"
          rowKey="id"
          loading={findings.isLoading}
          dataSource={findings.data?.findings ?? []}
          pagination={{ pageSize: 10, size: 'small' }}
          expandable={{
            expandedRowRender: (r: Finding) => (
              <pre className="mono">{JSON.stringify({ evidence: r.evidence, impact: r.impact }, null, 2)}</pre>
            ),
          }}
          columns={[
            { title: '规则', dataIndex: 'ruleCode', width: 110 },
            {
              title: '结论',
              dataIndex: 'verdict',
              width: 130,
              render: (v: string) => (
                <StatusPill tone={VERDICT_TONE[v] ?? 'neutral'} size="sm" dot>
                  {v}
                </StatusPill>
              ),
            },
            { title: '严重度', dataIndex: 'severity', width: 80 },
            { title: '标题', dataIndex: 'title' },
            { title: '对象', dataIndex: 'objectKey', ellipsis: true },
            { title: '说明', dataIndex: 'detail', ellipsis: true },
          ]}
        />
      </Drawer>

      <Modal
        open={!!report}
        width={920}
        title={`巡检报告 · ${report?.runId ?? ''}`}
        footer={null}
        onCancel={() => setReport(null)}
      >
        <Typography.Paragraph copyable={{ text: report?.content }}>
          可复制为 Markdown 存档
        </Typography.Paragraph>
        <pre className="mono" style={{ maxHeight: 560, overflow: 'auto' }}>
          {report?.content}
        </pre>
      </Modal>
    </>
  );
}
