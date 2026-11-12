import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import {
  AlertOutlined,
  ApiOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { api } from '../api';
import type { AlarmEvent, BizItem, BizMetric, BizOverview, BizRule } from '../types';

const SEVERITY_COLOR: Record<string, string> = {
  critical: 'red',
  warning: 'orange',
  info: 'blue',
};

const SEVERITY_LABEL: Record<string, string> = {
  critical: '严重',
  warning: '警告',
  info: '提示',
};

interface MetricMeta {
  label: string;
  unit: string;
  scale: number;
  defaultThreshold: number;
  desc: string;
}

const METRIC_META: Record<BizMetric, MetricMeta> = {
  error_rate: {
    label: '错误率',
    unit: '%',
    scale: 100,
    defaultThreshold: 0.05,
    desc: '错误率建议阈值 0.05（即 5%），超过视为接口异常',
  },
  tp99: {
    label: 'TP99',
    unit: '秒',
    scale: 1,
    defaultThreshold: 0.5,
    desc: 'TP99 建议阈值 0.5 秒，超过视为响应变慢',
  },
  request_total: {
    label: '请求量',
    unit: 'req/s',
    scale: 1,
    defaultThreshold: 10,
    desc: '请求量建议阈值 10 req/s，低于该值视为流量异常下跌',
  },
};

const METRIC_OPTIONS = (Object.keys(METRIC_META) as BizMetric[]).map((m) => ({
  value: m,
  label: `${METRIC_META[m].label}（${m}）`,
}));

function fmtTime(v?: string): string {
  if (!v) return '-';
  return v.replace('T', ' ').slice(0, 19);
}

/** 按指标单位格式化数值，统一保留 4 位小数；比率类按百分比展示。 */
function fmtMetric(v: number, metric: BizMetric): string {
  const meta = METRIC_META[metric] ?? METRIC_META.error_rate;
  return `${(v * meta.scale).toFixed(4)} ${meta.unit}`;
}

function conditionText(r: { metric: BizMetric; comparator: string; threshold: number }): string {
  return `${METRIC_META[r.metric]?.label ?? r.metric} ${r.comparator === 'lt' ? '<' : '>'} ${fmtMetric(
    r.threshold,
    r.metric,
  )}`;
}

interface RuleFormValues {
  name: string;
  appId: string;
  metric: BizMetric;
  comparator: 'gt' | 'lt';
  threshold: number;
  severity: 'critical' | 'warning' | 'info';
  windowMin: number;
}

/**
 * 外部业务接口监控：按规则持续检测错误率 / 请求量 / TP99，超阈值即写入告警中心。
 * 告警与排障、工单共用同一张 alarm_event 表（Source=biz）。
 */
export default function BizMonitor() {
  const qc = useQueryClient();
  const { message } = AntdApp.useApp();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm<RuleFormValues>();
  const selectedMetric = Form.useWatch('metric', form) ?? 'error_rate';

  const apps = useQuery({ queryKey: ['apps-biz'], queryFn: () => api.topoApps() });

  const { data, isFetching, refetch, error } = useQuery<BizOverview>({
    queryKey: ['biz-overview'],
    queryFn: () => api.bizOverview(),
    refetchInterval: 30000,
  });

  const scan = useMutation({
    mutationFn: () => api.bizScan(),
    onSuccess: (res) => {
      message.success(
        `扫描完成：${res.rules} 条规则，越界 ${res.breaches} 项，新建告警 ${res.created} 条`,
      );
      qc.invalidateQueries({ queryKey: ['biz-overview'] });
      qc.invalidateQueries({ queryKey: ['alarms'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const createRule = useMutation({
    mutationFn: (v: RuleFormValues) => api.bizCreateRule({ ...v, enabled: true }),
    onSuccess: () => {
      message.success('规则已创建');
      setOpen(false);
      form.resetFields();
      qc.invalidateQueries({ queryKey: ['biz-overview'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const deleteRule = useMutation({
    mutationFn: (id: string) => api.bizDeleteRule(id),
    onSuccess: () => {
      message.success('规则已删除');
      qc.invalidateQueries({ queryKey: ['biz-overview'] });
    },
    onError: (e: Error) => message.error(e.message),
  });

  const openCreate = () => {
    form.setFieldsValue({
      metric: 'error_rate',
      comparator: 'gt',
      threshold: METRIC_META.error_rate.defaultThreshold,
      severity: 'warning',
      windowMin: 5,
    });
    setOpen(true);
  };

  const ruleColumns = [
    {
      title: '规则名称',
      dataIndex: 'name',
      width: 180,
      render: (v: string) => <Typography.Text strong>{v || '-'}</Typography.Text>,
    },
    { title: '应用', dataIndex: 'appId', width: 150, ellipsis: true },
    {
      title: '指标',
      dataIndex: 'metric',
      width: 120,
      render: (v: BizMetric) => <Tag color="blue">{METRIC_META[v]?.label ?? v}</Tag>,
    },
    {
      title: '条件',
      width: 200,
      render: (_: unknown, r: BizRule) => (
        <Typography.Text code style={{ fontSize: 12 }}>
          {conditionText(r)}
        </Typography.Text>
      ),
    },
    {
      title: '级别',
      dataIndex: 'severity',
      width: 80,
      render: (v: string) => <Tag color={SEVERITY_COLOR[v] ?? 'default'}>{SEVERITY_LABEL[v] ?? v}</Tag>,
    },
    {
      title: '窗口',
      dataIndex: 'windowMin',
      width: 80,
      render: (v: number) => `${v} 分钟`,
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 80,
      render: (v: boolean) => <Tag color={v ? 'green' : 'default'}>{v ? '启用' : '停用'}</Tag>,
    },
    {
      title: '操作',
      width: 90,
      render: (_: unknown, r: BizRule) => (
        <Popconfirm
          title="确认删除该规则？"
          description="删除后下一轮扫描不再检测该指标"
          okText="删除"
          cancelText="取消"
          onConfirm={() => deleteRule.mutate(r.id)}
        >
          <Button size="small" danger type="link">
            删除
          </Button>
        </Popconfirm>
      ),
    },
  ];

  const itemColumns = [
    { title: '规则', dataIndex: 'ruleName', width: 160, ellipsis: true },
    { title: '应用', dataIndex: 'appId', width: 140, ellipsis: true },
    {
      title: '指标',
      dataIndex: 'metric',
      width: 110,
      render: (v: BizMetric) => METRIC_META[v]?.label ?? v,
    },
    {
      title: '实际值',
      dataIndex: 'value',
      width: 140,
      render: (v: number, r: BizItem) => (
        <span style={{ color: r.breach ? '#C41E1A' : undefined }}>{fmtMetric(v, r.metric)}</span>
      ),
    },
    {
      title: '阈值',
      width: 140,
      render: (_: unknown, r: BizItem) => fmtMetric(r.threshold, r.metric),
    },
    {
      title: '是否越界',
      dataIndex: 'breach',
      width: 90,
      render: (v: boolean) => <Tag color={v ? 'red' : 'green'}>{v ? '越界' : '正常'}</Tag>,
    },
    {
      title: '错误',
      dataIndex: 'error',
      ellipsis: true,
      render: (v?: string) =>
        v ? <Typography.Text type="danger" style={{ fontSize: 12 }}>{v}</Typography.Text> : '-',
    },
  ];

  const firingColumns = [
    {
      title: '告警名称',
      dataIndex: 'alertName',
      width: 170,
      render: (v: string) => <Tag color="gold">{v}</Tag>,
    },
    { title: '实例', dataIndex: 'instance', width: 150, ellipsis: true },
    { title: '主机', dataIndex: 'host', width: 140, ellipsis: true },
    {
      title: '级别',
      dataIndex: 'severity',
      width: 80,
      render: (v: string) => <Tag color={SEVERITY_COLOR[v] ?? 'default'}>{SEVERITY_LABEL[v] ?? v}</Tag>,
    },
    {
      title: '首见时间',
      dataIndex: 'firstSeenAt',
      width: 160,
      render: (v: string) => <span style={{ fontSize: 12 }}>{fmtTime(v)}</span>,
    },
    {
      title: '操作',
      width: 120,
      render: (_: unknown, r: AlarmEvent) => (
        <Link to={`/alert-center?q=${encodeURIComponent(r.host || r.alertName)}`}>查看告警中心</Link>
      ),
    },
  ];

  const last = data?.lastScan;

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <ApiOutlined style={{ color: '#1D4ED8' }} />
          <Typography.Text strong>外部业务接口监控</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            按规则持续检测错误率 / 请求量 / TP99，超阈值自动写入告警中心，与排障 / 工单共用同一套去重与恢复
          </Typography.Text>
          <div style={{ flex: 1 }} />
          <Button icon={<PlusOutlined />} onClick={openCreate}>
            新建规则
          </Button>
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            loading={scan.isPending}
            onClick={() => scan.mutate()}
          >
            立即扫描
          </Button>
          <Button icon={<ReloadOutlined />} loading={isFetching} onClick={() => refetch()}>
            刷新
          </Button>
        </Space>
      </Card>

      {error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message={(error as Error).message} />
      )}

      <Row gutter={[12, 12]}>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="监控规则数"
              value={data?.ruleCount ?? data?.rules.length ?? 0}
              prefix={<ApiOutlined />}
              valueStyle={{ color: '#1D4ED8' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              覆盖 {new Set((data?.rules ?? []).map((r) => r.appId)).size} 个应用
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="本轮越界数"
              value={last?.breaches ?? 0}
              prefix={<AlertOutlined />}
              valueStyle={{ color: '#C41E1A' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {last ? `共 ${last.rules} 条规则参与扫描` : '尚未扫描'}
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="当前告警数"
              value={data?.firing.length ?? 0}
              valueStyle={{ color: '#C77700' }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              来源 biz 的 firing 事件
            </Typography.Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card size="small">
            <Statistic
              title="最近扫描时间"
              value={last ? fmtTime(last.at) : '尚未扫描'}
              valueStyle={{ fontSize: 18 }}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {last ? `本轮新建 ${last.created} 条告警` : '30 秒自动刷新概况'}
            </Typography.Text>
          </Card>
        </Col>
      </Row>

      <Card size="small" title={`监控规则（${data?.rules.length ?? 0}）`} style={{ marginTop: 12 }}>
        <Table
          rowKey="id"
          size="small"
          loading={!data && isFetching}
          columns={ruleColumns}
          dataSource={data?.rules ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty description="暂无监控规则，点击右上角新建" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
        />
      </Card>

      <Card
        size="small"
        title="扫描明细"
        style={{ marginTop: 12 }}
        extra={
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {last ? `${fmtTime(last.at)} · ${last.items.length} 项` : '尚未扫描'}
          </Typography.Text>
        }
      >
        <Table
          rowKey={(r: BizItem) => `${r.ruleId}|${r.appId}`}
          size="small"
          loading={!data && isFetching}
          columns={itemColumns}
          dataSource={last?.items ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty description="暂无扫描明细" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
        />
      </Card>

      <Card
        size="small"
        title={`当前告警（${data?.firing.length ?? 0}）`}
        style={{ marginTop: 12 }}
        extra={<Link to="/alert-center">去告警中心</Link>}
      >
        <Table
          rowKey="fingerprint"
          size="small"
          loading={!data && isFetching}
          columns={firingColumns}
          dataSource={data?.firing ?? []}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          locale={{ emptyText: <Empty description="当前没有业务接口告警" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
        />
      </Card>

      <Modal
        title="新建监控规则"
        open={open}
        okText="创建"
        cancelText="取消"
        confirmLoading={createRule.isPending}
        onOk={() => form.submit()}
        onCancel={() => setOpen(false)}
        destroyOnHidden
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(v) => createRule.mutate(v)}
          onValuesChange={(changed: Partial<RuleFormValues>) => {
            if (changed.metric) {
              form.setFieldsValue({ threshold: METRIC_META[changed.metric].defaultThreshold });
            }
          }}
        >
          <Form.Item name="name" label="规则名称" rules={[{ required: true, message: '请输入规则名称' }]}>
            <Input placeholder="如：订单接口错误率" />
          </Form.Item>
          <Form.Item name="appId" label="应用" rules={[{ required: true, message: '请选择应用' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择应用"
              loading={apps.isFetching}
              options={(apps.data?.apps ?? []).map((a) => ({
                value: a.appId,
                label: a.appName ? `${a.appName}（${a.appId}）` : a.appId,
              }))}
            />
          </Form.Item>
          <Form.Item name="metric" label="指标" rules={[{ required: true, message: '请选择指标' }]}>
            <Select options={METRIC_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="threshold"
            label="阈值"
            extra={METRIC_META[selectedMetric]?.desc}
            rules={[{ required: true, message: '请输入阈值' }]}
          >
            <InputNumber style={{ width: '100%' }} min={0} step={0.01} />
          </Form.Item>
          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="comparator" label="比较方式" rules={[{ required: true }]}>
                <Select
                  options={[
                    { value: 'gt', label: '大于（>）' },
                    { value: 'lt', label: '小于（<）' },
                  ]}
                />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="severity" label="级别" rules={[{ required: true }]}>
                <Select
                  options={[
                    { value: 'critical', label: '严重' },
                    { value: 'warning', label: '警告' },
                    { value: 'info', label: '提示' },
                  ]}
                />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="windowMin" label="统计窗口（分钟）" rules={[{ required: true }]}>
            <InputNumber style={{ width: '100%' }} min={1} max={1440} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
