import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  App as AntdApp,
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Empty,
  Input,
  List,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import { BookOutlined, ReloadOutlined, SearchOutlined, SyncOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { api } from '../api';
import type { KnowledgeCase } from '../types';

const STATUS_TEXT: Record<string, string> = {
  root_caused: '已定位根因',
  investigating: '排查中',
  failed: '失败',
  done: '已完成',
};

function fmtTime(v?: string): string {
  if (!v) return '-';
  return v.replace('T', ' ').slice(0, 19);
}

function confidenceColor(v: number): string {
  if (v >= 0.8) return 'green';
  if (v >= 0.6) return 'blue';
  if (v >= 0.4) return 'orange';
  return 'default';
}

/**
 * 历史故障知识库：把已定位根因 / 已标注的排障会话归档为案例，
 * 支持关键词检索、相似案例推荐与证据链回看。
 */
export default function Knowledge() {
  const qc = useQueryClient();
  const { message } = AntdApp.useApp();
  const [q, setQ] = useState('');
  const [appId, setAppId] = useState<string | undefined>();
  const [active, setActive] = useState<KnowledgeCase | null>(null);

  const apps = useQuery({ queryKey: ['apps-knowledge'], queryFn: () => api.topoApps() });
  const stats = useQuery({ queryKey: ['knowledge-stats'], queryFn: () => api.knowledgeStats() });
  const cases = useQuery({
    queryKey: ['knowledge', q, appId ?? ''],
    queryFn: () => api.knowledgeList({ q, appId, limit: 100 }),
  });

  const reindex = useMutation({
    mutationFn: () => api.knowledgeReindex(),
    onSuccess: (r) => {
      message.success(`归档完成：${r.indexed} 条案例`);
      qc.invalidateQueries({ queryKey: ['knowledge'] });
      qc.invalidateQueries({ queryKey: ['knowledge-stats'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const columns = [
    {
      title: '案例',
      key: 'title',
      width: 280,
      render: (_: unknown, r: KnowledgeCase) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong style={{ fontSize: 13 }}>
            {r.title || '未命名案例'}
          </Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 11, maxWidth: 260 }} ellipsis>
            {r.symptom}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '应用',
      dataIndex: 'appId',
      width: 170,
      ellipsis: true,
      render: (v: string | undefined, r: KnowledgeCase) => (
        <Typography.Text style={{ fontSize: 12 }}>{v || '-'}</Typography.Text>
      ),
    },
    {
      title: '中间件',
      key: 'mw',
      width: 170,
      render: (_: unknown, r: KnowledgeCase) => (
        <Typography.Text style={{ fontSize: 12 }}>
          {r.middlewareType || '-'}
          {r.addr ? `(${r.addr})` : ''}
        </Typography.Text>
      ),
    },
    {
      title: '根因',
      dataIndex: 'rootCause',
      width: 240,
      ellipsis: true,
      render: (v: string | undefined) =>
        v ? (
          <Typography.Text style={{ fontSize: 12 }}>{v}</Typography.Text>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            未定位
          </Typography.Text>
        ),
    },
    {
      title: '置信度',
      dataIndex: 'confidence',
      width: 100,
      render: (v: number) => <Tag color={confidenceColor(v)}>{v.toFixed(2)}</Tag>,
    },
    {
      title: '标注',
      key: 'reviewed',
      width: 110,
      render: (_: unknown, r: KnowledgeCase) => {
        if (!r.reviewed) return <Typography.Text type="secondary">未标注</Typography.Text>;
        return r.accurate ? <Tag color="green">准确</Tag> : <Tag color="red">不准确</Tag>;
      },
    },
    {
      title: '工单',
      dataIndex: 'ticketId',
      width: 100,
      render: (v: string | undefined) =>
        v ? <Link to="/tickets">查看</Link> : <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '时间',
      dataIndex: 'createdAt',
      width: 170,
      render: (v: string) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {fmtTime(v)}
        </Typography.Text>
      ),
    },
    {
      title: '操作',
      key: 'ops',
      width: 90,
      render: (_: unknown, r: KnowledgeCase) => (
        <Button size="small" type="link" onClick={() => setActive(r)}>
          详情
        </Button>
      ),
    },
  ];

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <BookOutlined style={{ color: '#1A4E8A' }} />
          <Typography.Text strong>历史故障知识库</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            已定位根因 / 已标注的排障会话自动归档，支持关键词检索与相似案例推荐
          </Typography.Text>
          <div style={{ flex: 1 }} />
          <Input.Search
            style={{ width: 280 }}
            allowClear
            placeholder="搜索现象 / 根因 / 应用（空格分词）"
            enterButton={<SearchOutlined />}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onSearch={(v) => setQ(v)}
          />
          <Select
            style={{ width: 240 }}
            showSearch
            allowClear
            optionFilterProp="label"
            placeholder="按应用筛选"
            value={appId}
            loading={apps.isFetching}
            onChange={(v?: string) => setAppId(v)}
            options={(apps.data?.apps ?? []).map((a) => ({
              value: a.appId,
              label: a.appName ? `${a.appName}（${a.appId}）` : a.appId,
            }))}
          />
          <Button
            icon={<SyncOutlined />}
            loading={reindex.isPending}
            onClick={() => reindex.mutate()}
          >
            重新归档
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={cases.isFetching}
            onClick={() => {
              cases.refetch();
              stats.refetch();
            }}
          >
            刷新
          </Button>
        </Space>
      </Card>

      <Row gutter={[12, 12]} style={{ marginBottom: 12 }}>
        {(
          [
            ['total', '案例总数'],
            ['apps', '覆盖应用'],
            ['reviewed', '已标注'],
            ['accuracy', '标注准确率'],
          ] as const
        ).map(([key, label]) => (
          <Col xs={12} sm={6} key={key}>
            <Card size="small">
              <Statistic
                title={label}
                value={
                  key === 'accuracy'
                    ? (stats.data?.accuracy ?? 0).toFixed(1)
                    : stats.data?.[key] ?? 0
                }
                suffix={key === 'accuracy' ? '%' : undefined}
              />
            </Card>
          </Col>
        ))}
      </Row>

      <Card size="small" title={`案例（${cases.data?.total ?? 0}）`}>
        <Table
          rowKey="id"
          size="small"
          loading={cases.isFetching}
          columns={columns}
          dataSource={cases.data?.cases ?? []}
          scroll={{ x: 1430 }}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  <span>
                    暂无归档案例，请先在<Link to="/diagnosis"> 排障工作台 </Link>
                    完成一次定位根因或标注准确性的排障
                  </span>
                }
              />
            ),
          }}
        />
      </Card>

      {stats.data?.topApps && stats.data.topApps.length > 0 && (
        <Card size="small" title="Top 应用" style={{ marginTop: 12 }}>
          <Space wrap>
            {stats.data.topApps.map((a) => (
              <Tag
                key={a.appId}
                color="blue"
                style={{ cursor: 'pointer' }}
                onClick={() => setAppId(a.appId)}
              >
                {a.appId} · {a.count}
              </Tag>
            ))}
          </Space>
        </Card>
      )}

      <Drawer
        width={640}
        open={!!active}
        onClose={() => setActive(null)}
        title={active?.title || '案例详情'}
      >
        {active && (
          <Space direction="vertical" size={14} style={{ width: '100%' }}>
            <Descriptions size="small" column={2} bordered>
              <Descriptions.Item label="案例 ID" span={2}>
                <Typography.Text copyable style={{ fontSize: 12 }}>
                  {active.id}
                </Typography.Text>
              </Descriptions.Item>
              <Descriptions.Item label="应用">{active.appId || '-'}</Descriptions.Item>
              <Descriptions.Item label="中间件">
                {active.middlewareType || '-'}
                {active.addr ? `(${active.addr})` : ''}
              </Descriptions.Item>
              <Descriptions.Item label="状态">
                {STATUS_TEXT[active.status] ?? active.status}
              </Descriptions.Item>
              <Descriptions.Item label="置信度">
                <Tag color={confidenceColor(active.confidence)}>
                  {active.confidence.toFixed(2)}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="现象" span={2}>
                {active.symptom || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="根因" span={2}>
                {active.rootCause || '-'}
              </Descriptions.Item>
              {active.reviewed && (
                <Descriptions.Item label="人工标注" span={2}>
                  {active.accurate ? (
                    <Tag color="green">准确</Tag>
                  ) : (
                    <Tag color="red">不准确</Tag>
                  )}
                  {active.actualRootCause ? ` · 实际根因：${active.actualRootCause}` : ''}
                </Descriptions.Item>
              )}
              <Descriptions.Item label="创建时间">{fmtTime(active.createdAt)}</Descriptions.Item>
              <Descriptions.Item label="完成时间">{fmtTime(active.finishedAt)}</Descriptions.Item>
              <Descriptions.Item label="关联工单" span={2}>
                {active.ticketId ? `${active.ticketId}（${active.ticketStatus ?? '-'}）` : '-'}
              </Descriptions.Item>
            </Descriptions>

            {active.keywords && active.keywords.length > 0 && (
              <div>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  检索关键词
                </Typography.Text>
                <div style={{ marginTop: 6 }}>
                  {active.keywords.map((k) => (
                    <Tag key={k}>{k}</Tag>
                  ))}
                </div>
              </div>
            )}

            <div>
              <Typography.Text strong style={{ fontSize: 13 }}>
                证据链（{active.evidence?.length ?? 0}）
              </Typography.Text>
              <List
                size="small"
                style={{ marginTop: 6 }}
                dataSource={active.evidence ?? []}
                locale={{ emptyText: '暂无证据链' }}
                renderItem={(e) => (
                  <List.Item style={{ paddingLeft: 0 }}>
                    <Space size={8} style={{ width: '100%' }}>
                      <Tag>#{e.no}</Tag>
                      <Tag color={e.status === 'ok' ? 'green' : e.status === 'skipped' ? 'default' : 'red'}>
                        {e.source}
                      </Tag>
                      <Typography.Text style={{ fontSize: 12, flex: 1 }}>
                        {e.title}
                        {e.error ? ` · ${e.error}` : ''}
                      </Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                        {e.latencyMs}ms
                      </Typography.Text>
                    </Space>
                  </List.Item>
                )}
              />
            </div>

            <Space>
              <Link
                to={`/diagnosis?appId=${encodeURIComponent(active.appId || '')}&mwType=${encodeURIComponent(
                  active.middlewareType || '',
                )}&addr=${encodeURIComponent(active.addr || '')}`}
              >
                以该对象重新排障
              </Link>
            </Space>
          </Space>
        )}
      </Drawer>
    </>
  );
}
