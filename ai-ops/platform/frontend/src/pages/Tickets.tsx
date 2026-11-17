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
import type { Ticket, TicketEvent, TicketStatus } from '../types';
import { useAuth } from '../AuthContext';

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

const ACTION_LABEL: Record<string, string> = {
  created: '建单',
  claim: '认领',
  assign: '指派',
  reassign: '改派',
  start: '开始处置',
  resolve: '标记已处置',
  auto_resolve: '告警恢复自动处置',
  close: '关闭',
  reopen: '重开',
};

const STATUS_FLOW: TicketStatus[] = ['open', 'assigned', 'in_progress', 'resolved', 'closed'];

/** 秒数转「x 小时 y 分钟」，用于 MTTA / MTTR 展示。 */
function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return '—';
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  if (h > 0) return `${h} 小时 ${m} 分钟`;
  return `${m} 分钟`;
}

export default function Tickets() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';
  const canWrite = user?.role === 'admin' || user?.role === 'ops';

  const [viewing, setViewing] = useState<Ticket | null>(null);
  const [note, setNote] = useState('');
  const [statusFilter, setStatusFilter] = useState<TicketStatus | undefined>();
  const [priorityFilter, setPriorityFilter] = useState<string | undefined>();

  const tickets = useQuery({
    queryKey: ['tickets', statusFilter, priorityFilter],
    queryFn: () => api.ticketList(200),
    // 后端重启/短暂不可用后能自愈，不用手动刷新页面。
    refetchInterval: 15000,
  });

  // 处理人来自真实用户体系，仅管理员指派时使用
  const opsUsers = useQuery({
    queryKey: ['users', 'ops'],
    queryFn: () => api.userList('ops'),
    enabled: isAdmin,
  });

  const filtered = (tickets.data?.tickets ?? []).filter((t) => {
    if (statusFilter && t.status !== statusFilter) return false;
    if (priorityFilter && t.priority !== priorityFilter) return false;
    return true;
  });

  const action = useMutation({
    mutationFn: (fn: () => Promise<Ticket>) => fn(),
    onSuccess: (updated) => {
      setViewing(updated);
      setNote('');
      message.success('操作成功');
      qc.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  /** 处理人本人或管理员可推进工单 */
  const canHandle = (t: Ticket) => !!user && (isAdmin || (!!t.assigneeId && t.assigneeId === user.id));

  /** 指派下拉：ops 与管理员均可作为处理人，排除只读账号 */
  const assigneeOptions = (opsUsers.data?.users ?? []).map((u) => ({
    value: u.id,
    label: u.displayName || u.username,
  }));

  const renderActions = (t: Ticket) => {
    if (!canWrite) {
      return <Typography.Text type="secondary">只读账号不可操作工单</Typography.Text>;
    }
    return (
      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        <Input.TextArea
          rows={2}
          value={note}
          placeholder="操作说明（可选，例如处置过程 / 关闭原因）"
          onChange={(e) => setNote(e.target.value)}
        />
        <Space wrap>
          {t.status === 'open' && (
            <Button
              size="small"
              type="primary"
              loading={action.isPending}
              onClick={() => action.mutate(() => api.ticketClaim(t.id, t.version))}
            >
              认领
            </Button>
          )}
          {t.status === 'assigned' && canHandle(t) && (
            <Button
              size="small"
              type="primary"
              loading={action.isPending}
              onClick={() => action.mutate(() => api.ticketStart(t.id, t.version))}
            >
              开始处置
            </Button>
          )}
          {t.status === 'in_progress' && canHandle(t) && (
            <Button
              size="small"
              type="primary"
              loading={action.isPending}
              onClick={() => action.mutate(() => api.ticketResolve(t.id, note, t.version))}
            >
              标记已处置
            </Button>
          )}
          {t.status === 'resolved' && canHandle(t) && (
            <Button
              size="small"
              danger
              loading={action.isPending}
              onClick={() => action.mutate(() => api.ticketClose(t.id, note, t.version))}
            >
              确认关闭
            </Button>
          )}
          {isAdmin && (t.status === 'resolved' || t.status === 'closed') && (
            <Button
              size="small"
              loading={action.isPending}
              onClick={() => action.mutate(() => api.ticketReopen(t.id, note, t.version))}
            >
              重开
            </Button>
          )}
        </Space>
        {isAdmin && (t.status === 'open' || t.status === 'assigned') && (
          <Select
            key={t.version}
            style={{ width: 220 }}
            placeholder={t.status === 'open' ? '指派处理人' : '改派处理人'}
            loading={opsUsers.isLoading}
            options={assigneeOptions}
            onChange={(assigneeId) =>
              action.mutate(() => api.ticketAssign(t.id, assigneeId, t.version, note))
            }
          />
        )}
        {!isAdmin && t.status === 'open' && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            认领后由你负责处置，管理员可改派
          </Typography.Text>
        )}
      </Space>
    );
  };

  const historyItems = (history: TicketEvent[] | undefined) =>
    (history ?? []).map((h) => ({
      color: h.to === 'closed' ? 'gray' : h.to === 'resolved' ? 'green' : 'blue',
      children: (
        <Space direction="vertical" size={0}>
          <Typography.Text>
            {ACTION_LABEL[h.action] ?? h.action}
            {h.from && h.to ? `：${STATUS_LABEL[h.from]} → ${STATUS_LABEL[h.to]}` : ''}
          </Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {h.actorName} · {new Date(h.at).toLocaleString()}
          </Typography.Text>
          {h.note && <Typography.Text style={{ fontSize: 12 }}>{h.note}</Typography.Text>}
        </Space>
      ),
    }));

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
      <Card
        size="small"
        title={
          tickets.isError && !tickets.data
            ? '工单列表（加载失败）'
            : `工单列表（${filtered.length} / ${tickets.data?.total ?? 0}）`
        }
      >
        {tickets.isError && tickets.data && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="刷新失败，当前展示的是上次成功加载的数据"
            description={(tickets.error as Error)?.message}
            action={
              <Button size="small" onClick={() => tickets.refetch()} loading={tickets.isFetching}>
                重试
              </Button>
            }
          />
        )}
        {tickets.isError && !tickets.data && (
          <Alert
            type="error"
            showIcon
            style={{ marginBottom: 12 }}
            message="工单列表加载失败"
            description={(tickets.error as Error)?.message ?? '请求后端接口失败，请确认后端服务是否在运行'}
            action={
              <Button size="small" onClick={() => tickets.refetch()} loading={tickets.isFetching}>
                重试
              </Button>
            }
          />
        )}
        {tickets.isLoading && <div style={{ textAlign: 'center', padding: 40 }}>加载中…</div>}
        {!tickets.isLoading && !tickets.isError && filtered.length === 0 && (
          <Empty description="暂无工单。先跑一次排障会话触发根因推断，工单会自动生成" />
        )}
        {(!tickets.isError || tickets.data) && filtered.length > 0 && (
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
                title: '处理人',
                width: 100,
                dataIndex: 'assignee',
                render: (v: string | undefined) =>
                  v || <Typography.Text type="secondary">待认领</Typography.Text>,
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
                title: '发现时间',
                width: 150,
                dataIndex: 'detectedAt',
                render: (v: string | undefined, t) => new Date(v ?? t.createdAt).toLocaleString(),
              },
              {
                title: '处置完成',
                width: 150,
                dataIndex: 'resolvedAt',
                render: (v: string | undefined) => (v ? new Date(v).toLocaleString() : '—'),
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
                <Descriptions.Item label="处理人">
                  {viewing.assignee || '待认领'}
                </Descriptions.Item>
                <Descriptions.Item label="重开次数">{viewing.reopenCount ?? 0}</Descriptions.Item>
                <Descriptions.Item label="发现时间">
                  {new Date(viewing.detectedAt ?? viewing.createdAt).toLocaleString()}
                </Descriptions.Item>
                <Descriptions.Item label="建单时间">{new Date(viewing.createdAt).toLocaleString()}</Descriptions.Item>
                <Descriptions.Item label="响应时长(MTTA)">{formatDuration(viewing.mttaSeconds)}</Descriptions.Item>
                <Descriptions.Item label="处置时长(MTTR)">{formatDuration(viewing.mttrSeconds)}</Descriptions.Item>
                <Descriptions.Item label="处置完成">
                  {viewing.resolvedAt ? new Date(viewing.resolvedAt).toLocaleString() : '—'}
                </Descriptions.Item>
                <Descriptions.Item label="关闭时间">
                  {viewing.closedAt ? new Date(viewing.closedAt).toLocaleString() : '—'}
                </Descriptions.Item>
                <Descriptions.Item label="更新时间" span={2}>{new Date(viewing.updatedAt).toLocaleString()}</Descriptions.Item>
              </Descriptions>
            </Card>

            <Card size="small" title="现象与根因" style={{ marginBottom: 12 }}>
              <Descriptions size="small" column={1} bordered>
                <Descriptions.Item label="现象">{viewing.symptom}</Descriptions.Item>
                <Descriptions.Item label="根因">{viewing.rootCause}</Descriptions.Item>
                {viewing.suggestion && <Descriptions.Item label="处置建议"><pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontFamily: 'inherit' }}>{viewing.suggestion}</pre></Descriptions.Item>}
              </Descriptions>
            </Card>

            {(viewing.findings ?? []).length > 0 && (
              <Card size="small" title="证据链摘要" style={{ marginBottom: 12 }}>
                <div style={{ maxHeight: 200, overflow: 'auto' }}>
                  {(viewing.findings ?? []).map((f, i) => (
                    <div key={i} style={{ fontSize: 12, padding: '2px 0', fontFamily: 'monospace' }}>{f}</div>
                  ))}
                </div>
              </Card>
            )}

            {/* 状态流转 + 动作区 */}
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

              <div style={{ marginTop: 12 }}>{renderActions(viewing)}</div>

              <div style={{ marginTop: 12 }}>
                <Button icon={<LinkOutlined />} onClick={() => navigate(viewing.evidenceRef)}>
                  查看完整证据链
                </Button>
              </div>
            </Card>

            <Card size="small" title="操作记录" style={{ marginBottom: 12 }}>
              {(viewing.history ?? []).length === 0 ? (
                <Typography.Text type="secondary">暂无操作记录</Typography.Text>
              ) : (
                <Timeline items={historyItems(viewing.history)} />
              )}
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
