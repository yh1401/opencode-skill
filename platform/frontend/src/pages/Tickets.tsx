import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Input,
  Row,
  Select,
  Space,
  Table,
  Timeline,
  Typography,
} from 'antd';
import { LinkOutlined } from '@ant-design/icons';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type { Ticket, TicketStatus } from '../types';

const PRIORITY_TONE: Record<string, PillTone> = {
  P0: 'risk',
  P1: 'warn',
  P2: 'info',
  P3: 'neutral',
};

const STATUS_COLOR: Record<TicketStatus, string> = {
  open: 'blue',
  assigned: 'cyan',
  in_progress: 'processing',
  resolved: 'success',
  closed: 'default',
};

const STATUS_TONE: Record<TicketStatus, PillTone> = {
  open: 'info',
  assigned: 'info',
  in_progress: 'info',
  resolved: 'ok',
  closed: 'neutral',
};

const STATUS_LABEL: Record<TicketStatus, string> = {
  open: '待处理',
  assigned: '已指派',
  in_progress: '处置中',
  resolved: '已处置',
  closed: '已关闭',
};

const STATUS_FLOW: TicketStatus[] = ['open', 'assigned', 'in_progress', 'resolved', 'closed'];

export default function Tickets() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [viewing, setViewing] = useState<Ticket | null>(null);
  const [statusFilter, setStatusFilter] = useState<TicketStatus | undefined>();
  const [priorityFilter, setPriorityFilter] = useState<string | undefined>();

  const tickets = useQuery({
    queryKey: ['tickets', statusFilter, priorityFilter],
    queryFn: () => api.ticketList(200),
  });

  const filtered = (tickets.data?.tickets ?? []).filter((t) => {
    if (statusFilter && t.status !== statusFilter) return false;
    if (priorityFilter && t.priority !== priorityFilter) return false;
    return true;
  });

  const statusChange = useMutation({
    mutationFn: (args: { id: string; status: TicketStatus; assignee?: string }) =>
      api.ticketUpdateStatus(args.id, args.status, args.assignee),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['tickets'] });
      message.success('状态已更新');
      setViewing(null);
    },
    onError: (e: Error) => message.error(e.message),
  });

  const resolve = useMutation({
    mutationFn: (id: string) => api.ticketResolve(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['tickets'] });
      message.success('已标记处置完成');
    },
    onError: (e: Error) => message.error(e.message),
  });

  return (
    <>
      {/* 顶部过滤条 */}
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space>
          <Typography.Text type="secondary">状态过滤:</Typography.Text>
          <Select
            placeholder="全部"
            style={{ width: 130 }}
            allowClear
            value={statusFilter}
            onChange={setStatusFilter}
            options={STATUS_FLOW.map((s) => ({ value: s, label: STATUS_LABEL[s] }))}
          />
          <Typography.Text type="secondary">优先级:</Typography.Text>
          <Select
            placeholder="全部"
            style={{ width: 110 }}
            allowClear
            value={priorityFilter}
            onChange={setPriorityFilter}
            options={['P0', 'P1', 'P2', 'P3'].map((p) => ({ value: p, label: p }))}
          />
        </Space>
      </Card>

      {/* 工单列表 */}
      <Card size="small" title={`工单列表（${filtered.length} / ${tickets.data?.total ?? 0}）`}>
        {tickets.isLoading && <div style={{ textAlign: 'center', padding: 40 }}>加载中…</div>}
        {!tickets.isLoading && filtered.length === 0 && (
          <Empty description="暂无工单。先跑一次排障会话触发根因推断，工单会自动生成" />
        )}
        {filtered.length > 0 && (
          <Table
            size="small"
            rowKey="id"
            dataSource={filtered}
            pagination={{ pageSize: 10, size: 'small' }}
            columns={[
              {
                title: '优先级',
                width: 80,
                dataIndex: 'priority',
                render: (p: string) => (
                  <StatusPill tone={PRIORITY_TONE[p] ?? 'neutral'} size="sm" dot>{p}</StatusPill>
                ),
              },
              {
                title: '状态',
                width: 100,
                dataIndex: 'status',
                render: (s: TicketStatus) => (
                  <StatusPill tone={STATUS_TONE[s]} size="sm" dot>{STATUS_LABEL[s]}</StatusPill>
                ),
              },
              {
                title: '工单',
                ellipsis: true,
                render: (_, t) => (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>{t.title}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {t.appId ? `appId=${t.appId}` : `${t.mwType}:${t.addr}`}
                    </Typography.Text>
                  </Space>
                ),
              },
              {
                title: '置信度',
                width: 90,
                render: (_, t) => `${Math.round(t.confidence * 100)}%`,
              },
              {
                title: '来源',
                width: 100,
                dataIndex: 'sourceId',
                ellipsis: true,
              },
              {
                title: '创建时间',
                width: 150,
                dataIndex: 'createdAt',
                render: (v: string) => new Date(v).toLocaleString(),
              },
              {
                title: '操作',
                width: 140,
                render: (_, t) => (
                  <Space>
                    <Button size="small" type="link" onClick={() => setViewing(t)}>详情</Button>
                    <Button
                      size="small"
                      type="link"
                      icon={<LinkOutlined />}
                      onClick={() => navigate(t.evidenceRef)}
                    >
                      去原会话
                    </Button>
                  </Space>
                ),
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
        title={viewing ? `工单 ${viewing.id}` : ''}
      >
        {viewing && (
          <>
            <Card size="small" title="基本信息" style={{ marginBottom: 12 }}>
              <Descriptions size="small" column={2} bordered>
                <Descriptions.Item label="标题" span={2}>{viewing.title}</Descriptions.Item>
                <Descriptions.Item label="优先级"><StatusPill tone={PRIORITY_TONE[viewing.priority] ?? 'neutral'} dot>{viewing.priority}</StatusPill></Descriptions.Item>
                <Descriptions.Item label="状态"><StatusPill tone={STATUS_TONE[viewing.status]} dot>{STATUS_LABEL[viewing.status]}</StatusPill></Descriptions.Item>
                <Descriptions.Item label="置信度">{Math.round(viewing.confidence * 100)}%</Descriptions.Item>
                <Descriptions.Item label="来源会话">{viewing.sourceId}</Descriptions.Item>
                {viewing.appId && <Descriptions.Item label="应用">{viewing.appId}</Descriptions.Item>}
                {viewing.mwType && <Descriptions.Item label="中间件" span={viewing.appId ? 1 : 2}>{`${viewing.mwType}:${viewing.addr ?? '-'}`}</Descriptions.Item>}
                {viewing.assignee && <Descriptions.Item label="处理人" span={viewing.appId && viewing.mwType ? 0 : 2}>{viewing.assignee}</Descriptions.Item>}
                <Descriptions.Item label="创建时间">{new Date(viewing.createdAt).toLocaleString()}</Descriptions.Item>
                <Descriptions.Item label="更新时间">{new Date(viewing.updatedAt).toLocaleString()}</Descriptions.Item>
              </Descriptions>
            </Card>

            <Card size="small" title="现象与根因" style={{ marginBottom: 12 }}>
              <Descriptions size="small" column={1} bordered>
                <Descriptions.Item label="现象">{viewing.symptom}</Descriptions.Item>
                <Descriptions.Item label="根因">{viewing.rootCause}</Descriptions.Item>
                {viewing.suggestion && <Descriptions.Item label="处置建议"><pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontFamily: 'inherit' }}>{viewing.suggestion}</pre></Descriptions.Item>}
              </Descriptions>
            </Card>

            {viewing.findings.length > 0 && (
              <Card size="small" title="证据链摘要" style={{ marginBottom: 12 }}>
                <div style={{ maxHeight: 200, overflow: 'auto' }}>
                  {viewing.findings.map((f, i) => (
                    <div key={i} style={{ fontSize: 12, padding: '2px 0', fontFamily: 'monospace' }}>{f}</div>
                  ))}
                </div>
              </Card>
            )}

            {/* 状态流转 Timeline */}
            <Card size="small" title="处置进度" style={{ marginBottom: 12 }}>
              <Timeline
                items={STATUS_FLOW.map((s) => {
                  const idx = STATUS_FLOW.indexOf(viewing.status);
                  const curIdx = STATUS_FLOW.indexOf(s);
                  let color = 'gray';
                  if (curIdx < idx) color = 'green';
                  else if (curIdx === idx) color = STATUS_COLOR[s] as string;
                  return {
                    color,
                    children: (
                      <Space>
                        <Typography.Text type={curIdx > idx ? 'secondary' : undefined}>
                          {STATUS_LABEL[s]}
                        </Typography.Text>
                        {curIdx === idx && (
                          <StatusPill tone={STATUS_TONE[s]} dot>当前</StatusPill>
                        )}
                      </Space>
                    ),
                  };
                })}
              />

              <Row gutter={8} style={{ marginTop: 12 }}>
                <Space wrap>
                  {viewing.status !== 'closed' && (
                    <>
                      <Select
                        placeholder="指派处理人"
                        style={{ width: 160 }}
                        allowClear
                        options={['张三', '李四', '王五'].map((n) => ({ value: n, label: n }))}
                        onChange={(assignee) => {
                          statusChange.mutate({ id: viewing.id, status: viewing.status, assignee: assignee ?? undefined });
                        }}
                      />
                      {viewing.status === 'open' && (
                        <Button
                          size="small"
                          onClick={() => statusChange.mutate({ id: viewing.id, status: 'assigned' })}
                          loading={statusChange.isPending}
                        >
                          标记已指派
                        </Button>
                      )}
                      {(viewing.status === 'open' || viewing.status === 'assigned') && (
                        <Button
                          size="small"
                          type="primary"
                          onClick={() => statusChange.mutate({ id: viewing.id, status: 'in_progress' })}
                          loading={statusChange.isPending}
                        >
                          开始处置
                        </Button>
                      )}
                      {viewing.status !== 'resolved' && (
                        <Button
                          size="small"
                          type="primary"
                          onClick={() => resolve.mutate(viewing.id)}
                          loading={resolve.isPending}
                        >
                          标记已处置
                        </Button>
                      )}
                      <Button
                        size="small"
                        danger
                        onClick={() => statusChange.mutate({ id: viewing.id, status: 'closed' })}
                        loading={statusChange.isPending}
                      >
                        关闭工单
                      </Button>
                    </>
                  )}
                  <Button icon={<LinkOutlined />} onClick={() => navigate(viewing.evidenceRef)}>
                    查看完整证据链
                  </Button>
                </Space>
              </Row>
            </Card>

            {viewing.priority === 'P0' && (
              <Alert type="error" showIcon message="P0 级工单，建议优先处理" />
            )}
          </>
        )}
      </Drawer>
    </>
  );
}
