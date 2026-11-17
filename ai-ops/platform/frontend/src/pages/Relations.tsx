import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  Empty,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import {
  CheckOutlined,
  CloseOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
  WarningOutlined,
} from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { api } from '../api';
import type {
  RelationKind,
  RelationOverride,
  RelationProposal,
  RelationScanResult,
  RelationStatus,
} from '../types';

const KIND_META: Record<RelationKind, { color: string; text: string; hint: string }> = {
  missing: { color: 'blue', text: '依赖缺采', hint: '拓扑快照新增了依赖，建议确认后补录' },
  stale: { color: 'orange', text: '依赖失效', hint: '拓扑快照中依赖消失，建议确认后清理' },
  orphan: { color: 'red', text: 'CMDB 孤儿', hint: 'CMDB 有、拓扑无，需人工判断纳管或清退' },
};

const STATUS_META: Record<RelationStatus, { color: string; text: string }> = {
  pending: { color: 'gold', text: '待处理' },
  approved: { color: 'blue', text: '已通过' },
  rejected: { color: 'default', text: '已驳回' },
  applied: { color: 'green', text: '已执行' },
};

function fmtTime(v?: string): string {
  if (!v) return '-';
  return v.replace('T', ' ').slice(0, 19);
}

function targetOf(p: RelationProposal): string {
  if (p.kind === 'orphan') return p.connectAddr || '-';
  const app = p.appName ? `${p.appName}（${p.appId}）` : p.appId || '-';
  return `${app} → ${p.middlewareType || '-'}${p.connectAddr ? `(${p.connectAddr})` : ''}`;
}

/**
 * 关系修正：拓扑 Diff + 资源稽核孤儿 → 修正建议 → 审批 → 执行留痕。
 * 自动修正仅覆盖拓扑 Diff（missing / stale），孤儿只能人工确认。
 */
export default function Relations() {
  const qc = useQueryClient();
  const { message } = AntdApp.useApp();
  const [status, setStatus] = useState<string | undefined>('pending');
  const [kind, setKind] = useState<string | undefined>();
  const [from, setFrom] = useState<string | undefined>();
  const [to, setTo] = useState<string | undefined>();
  const [lastScan, setLastScan] = useState<RelationScanResult | null>(null);
  const [reviewTarget, setReviewTarget] = useState<{
    item: RelationProposal;
    status: 'approved' | 'rejected';
  } | null>(null);
  const [note, setNote] = useState('');

  const dates = useQuery({ queryKey: ['topo-dates'], queryFn: () => api.topoDates() });
  const stats = useQuery({ queryKey: ['relation-stats'], queryFn: () => api.relationStats() });
  const list = useQuery({
    queryKey: ['relations', status ?? '', kind ?? ''],
    queryFn: () => api.relationList({ status, kind }),
  });
  const overrides = useQuery({
    queryKey: ['relation-overrides'],
    queryFn: () => api.relationOverrides(),
  });

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['relations'] });
    qc.invalidateQueries({ queryKey: ['relation-stats'] });
    qc.invalidateQueries({ queryKey: ['relation-overrides'] });
  };

  const scan = useMutation({
    mutationFn: () => api.relationScan({ from, to }),
    onSuccess: (r) => {
      setLastScan(r);
      message.success(
        `扫描完成：新增建议 ${r.created} 条，重复跳过 ${r.skipped} 条，累计 ${r.total} 条`,
      );
      invalidate();
    },
    onError: (e: Error) => message.error(e.message),
  });

  const review = useMutation({
    mutationFn: () => {
      if (!reviewTarget) throw new Error('未选择建议');
      return api.relationUpdate(reviewTarget.item.id, reviewTarget.status, note);
    },
    onSuccess: () => {
      message.success(reviewTarget?.status === 'approved' ? '已通过' : '已驳回');
      setReviewTarget(null);
      setNote('');
      invalidate();
    },
    onError: (e: Error) => message.error(e.message),
  });

  const apply = useMutation({
    mutationFn: (id: string) => api.relationApply(id),
    onSuccess: (p) => {
      message.success(`已执行修正（${p.kind}）：${p.connectAddr || p.id}`);
      invalidate();
    },
    onError: (e: Error) => message.error(e.message),
  });

  const dateOptions = useMemo(
    () => (dates.data?.dates ?? []).map((d) => ({ value: d, label: d })),
    [dates.data],
  );

  const columns = [
    {
      title: '类型',
      dataIndex: 'kind',
      width: 110,
      render: (v: RelationKind) => (
        <Tooltip title={KIND_META[v]?.hint}>
          <Tag color={KIND_META[v]?.color}>{KIND_META[v]?.text ?? v}</Tag>
        </Tooltip>
      ),
    },
    {
      title: '对象 / 依赖',
      key: 'target',
      width: 300,
      ellipsis: true,
      render: (_: unknown, r: RelationProposal) => (
        <Typography.Text style={{ fontSize: 12 }}>{targetOf(r)}</Typography.Text>
      ),
    },
    {
      title: '来源',
      dataIndex: 'source',
      width: 100,
      render: (v: string) => (
        <Tag>{v === 'topo_diff' ? '拓扑 Diff' : '资源稽核'}</Tag>
      ),
    },
    {
      title: '说明 / 建议',
      key: 'detail',
      render: (_: unknown, r: RelationProposal) => (
        <Space direction="vertical" size={0}>
          <Typography.Text style={{ fontSize: 12 }}>{r.detail}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {r.suggestion}
          </Typography.Text>
          {r.note && (
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              审批备注：{r.note}
            </Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: '修正方式',
      dataIndex: 'autoFix',
      width: 110,
      render: (v: boolean) =>
        v ? <Tag color="cyan">可自动修正</Tag> : <Tag icon={<WarningOutlined />}>仅人工</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (v: RelationStatus) => (
        <Tag color={STATUS_META[v]?.color}>{STATUS_META[v]?.text ?? v}</Tag>
      ),
    },
    {
      title: '时间',
      key: 'time',
      width: 170,
      render: (_: unknown, r: RelationProposal) => (
        <Typography.Text style={{ fontSize: 12 }} type="secondary">
          创建 {fmtTime(r.createdAt)}
          {r.appliedAt ? ` / 执行 ${fmtTime(r.appliedAt)}` : ''}
        </Typography.Text>
      ),
    },
    {
      title: '操作',
      key: 'ops',
      width: 210,
      render: (_: unknown, r: RelationProposal) => {
        if (r.status === 'pending') {
          return (
            <Space size={4}>
              <Button
                size="small"
                type="link"
                icon={<CheckOutlined />}
                onClick={() => {
                  setNote('');
                  setReviewTarget({ item: r, status: 'approved' });
                }}
              >
                通过
              </Button>
              <Button
                size="small"
                type="link"
                danger
                icon={<CloseOutlined />}
                onClick={() => {
                  setNote('');
                  setReviewTarget({ item: r, status: 'rejected' });
                }}
              >
                驳回
              </Button>
            </Space>
          );
        }
        if (r.status === 'approved' && r.autoFix) {
          return (
            <Button
              size="small"
              type="link"
              icon={<PlayCircleOutlined />}
              loading={apply.isPending && apply.variables === r.id}
              onClick={() => apply.mutate(r.id)}
            >
              执行修正
            </Button>
          );
        }
        if (r.status === 'approved' && !r.autoFix) {
          return (
            <Tooltip title="该建议无法自动修正，请人工确认纳管或清退">
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                待人工处理
              </Typography.Text>
            </Tooltip>
          );
        }
        return <Typography.Text type="secondary">-</Typography.Text>;
      },
    },
  ];

  const overrideColumns = [
    {
      title: '执行时间',
      dataIndex: 'appliedAt',
      width: 170,
      render: (v: string) => <span style={{ fontSize: 12 }}>{fmtTime(v)}</span>,
    },
    {
      title: '动作',
      dataIndex: 'action',
      width: 100,
      render: (v: string) => (
        <Tag color={v === 'remove' ? 'orange' : 'blue'}>{v === 'remove' ? '清理' : '补录'}</Tag>
      ),
    },
    {
      title: '类型',
      dataIndex: 'kind',
      width: 110,
      render: (v: RelationKind) => (
        <Tag color={KIND_META[v]?.color}>{KIND_META[v]?.text ?? v}</Tag>
      ),
    },
    {
      title: '关系',
      key: 'target',
      ellipsis: true,
      render: (_: unknown, r: RelationOverride) => (
        <Typography.Text style={{ fontSize: 12 }}>
          {r.appName ? `${r.appName}（${r.appId}）` : r.appId || '-'} →{' '}
          {r.middlewareType || '-'}
          {r.connectAddr ? `(${r.connectAddr})` : ''}
        </Typography.Text>
      ),
    },
    {
      title: '建议 ID',
      dataIndex: 'proposalId',
      width: 180,
      render: (v: string) => (
        <Typography.Text copyable style={{ fontSize: 12 }}>
          {v}
        </Typography.Text>
      ),
    },
  ];

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <ThunderboltOutlined style={{ color: '#C77700' }} />
          <Typography.Text strong>关系修正</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            对比拓扑快照差异 + 复用最近一次资源稽核的 CMDB 孤儿，生成修正建议并留痕
          </Typography.Text>
          <div style={{ flex: 1 }} />
          <Select
            style={{ width: 140 }}
            allowClear
            placeholder="起始快照"
            value={from}
            options={dateOptions}
            onChange={(v?: string) => setFrom(v)}
          />
          <Select
            style={{ width: 140 }}
            allowClear
            placeholder="目标快照"
            value={to}
            options={dateOptions}
            onChange={(v?: string) => setTo(v)}
          />
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            loading={scan.isPending}
            onClick={() => scan.mutate()}
          >
            扫描差异
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={list.isFetching || stats.isFetching}
            onClick={() => {
              list.refetch();
              stats.refetch();
              overrides.refetch();
            }}
          >
            刷新
          </Button>
          <Select
            style={{ width: 120 }}
            allowClear
            placeholder="全部状态"
            value={status}
            onChange={(v?: string) => setStatus(v)}
            options={Object.entries(STATUS_META).map(([value, m]) => ({
              value,
              label: m.text,
            }))}
          />
          <Select
            style={{ width: 130 }}
            allowClear
            placeholder="全部类型"
            value={kind}
            onChange={(v?: string) => setKind(v)}
            options={Object.entries(KIND_META).map(([value, m]) => ({
              value,
              label: m.text,
            }))}
          />
        </Space>
      </Card>

      {lastScan && (
        <Alert
          type={lastScan.created > 0 ? 'success' : 'info'}
          showIcon
          closable
          onClose={() => setLastScan(null)}
          style={{ marginBottom: 12 }}
          message={`扫描 ${lastScan.from} → ${lastScan.to}：新增依赖 ${lastScan.added}、消失依赖 ${lastScan.removed}、稽核孤儿 ${lastScan.orphans}；新建建议 ${lastScan.created}，重复跳过 ${lastScan.skipped}`}
          description={
            lastScan.warnings?.length ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {lastScan.warnings.join('；')}
              </Typography.Text>
            ) : undefined
          }
        />
      )}
      {overrides.data && overrides.data.total > 0 && (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 12 }}
          message={`已执行修正 ${overrides.data.total} 条，记录见下方「修正留痕」`}
        />
      )}

      <Row gutter={[12, 12]} style={{ marginBottom: 12 }}>
        {(
          [
            ['pending', '待处理'],
            ['approved', '已通过'],
            ['applied', '已执行'],
            ['rejected', '已驳回'],
            ['autoFix', '可自动修正'],
            ['manualOnly', '仅人工'],
          ] as const
        ).map(([key, label]) => (
          <Col xs={12} sm={8} lg={4} key={key}>
            <Card size="small">
              <Statistic title={label} value={stats.data?.[key] ?? 0} />
            </Card>
          </Col>
        ))}
      </Row>

      <Card
        size="small"
        title={`修正建议（${list.data?.total ?? 0}）`}
        extra={
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            待处理优先 · 已执行建议不可再审批
          </Typography.Text>
        }
      >
        <Table
          rowKey="id"
          size="small"
          loading={list.isFetching}
          columns={columns}
          dataSource={list.data?.proposals ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  <span>
                    暂无建议，先点击「扫描差异」；若需孤儿建议请先执行
                    <Link to="/audit"> 资源稽核 </Link>
                  </span>
                }
              />
            ),
          }}
        />
      </Card>

      <Card
        size="small"
        title={`修正留痕（${overrides.data?.total ?? 0}）`}
        style={{ marginTop: 12 }}
      >
        <Table
          rowKey="proposalId"
          size="small"
          loading={overrides.isFetching}
          columns={overrideColumns}
          dataSource={overrides.data?.overrides ?? []}
          pagination={{ pageSize: 5, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无执行记录" /> }}
        />
      </Card>

      <Modal
        open={!!reviewTarget}
        title={reviewTarget?.status === 'approved' ? '通过修正建议' : '驳回修正建议'}
        okText={reviewTarget?.status === 'approved' ? '确认通过' : '确认驳回'}
        okButtonProps={{ danger: reviewTarget?.status === 'rejected' }}
        confirmLoading={review.isPending}
        onOk={() => review.mutate()}
        onCancel={() => {
          setReviewTarget(null);
          setNote('');
        }}
      >
        <Space direction="vertical" size={10} style={{ width: '100%' }}>
          <Typography.Text style={{ fontSize: 12 }}>
            {reviewTarget ? targetOf(reviewTarget.item) : ''}
          </Typography.Text>
          {reviewTarget && !reviewTarget.item.autoFix && (
            <Alert
              type="warning"
              showIcon
              message="该建议无法自动修正，通过后仍需人工在拓扑 / CMDB 侧处理"
            />
          )}
          <Input.TextArea
            rows={3}
            placeholder="审批备注（可选）"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </Space>
      </Modal>
    </>
  );
}
