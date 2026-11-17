import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Progress,
  Row,
  Segmented,
  Select,
  Space,
  Table,
  Typography,
} from 'antd';
import { LinkOutlined, PlusOutlined, WarningOutlined } from '@ant-design/icons';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type { ChangePlan, ChangeTemplate } from '../types';

const LEVEL_CONFIG: Record<string, { tone: PillTone; label: string; bg: string }> = {
  low: { tone: 'ok', label: '低风险', bg: '#f6ffed' },
  medium: { tone: 'info', label: '中风险', bg: '#e6f4ff' },
  high: { tone: 'warn', label: '高风险', bg: '#fff7e6' },
  critical: { tone: 'risk', label: '禁止执行', bg: '#fff1f0' },
};

const TARGET_LABELS: Record<string, string> = {
  middleware: '中间件',
  app: '应用',
  apollo: 'Apollo 配置',
  service: '服务',
  datacenter: '机房',
};

export default function ChangePlans() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [selectedTpl, setSelectedTpl] = useState<ChangeTemplate | null>(null);
  const [viewing, setViewing] = useState<ChangePlan | null>(null);

  const tpls = useQuery({ queryKey: ['change-templates'], queryFn: api.changeTemplates });
  const plans = useQuery({ queryKey: ['change-plans'], queryFn: () => api.changeList(30) });
  const dates = useQuery({ queryKey: ['change-dates'], queryFn: api.topoDates });

  const gen = useMutation({
    mutationFn: api.changeGenerate,
    onSuccess: (plan) => {
      qc.invalidateQueries({ queryKey: ['change-plans'] });
      message.success(`方案已生成，风险等级：${LEVEL_CONFIG[plan.risk.level].label}`);
      setViewing(plan);
    },
    onError: (e: Error) => message.error(e.message),
  });

  const onTemplateSelect = (tpl: ChangeTemplate) => {
    setSelectedTpl(tpl);
    const patch: Record<string, string> = {
      templateId: tpl.id,
      targetType: tpl.targetType,
      env: tpl.defaultEnv,
      dateToday: dates.data?.dates?.[dates.data.dates.length - 1] ?? '',
      datePrev: dates.data?.dates?.[dates.data.dates.length - 2] ?? '',
    };
    form.setFieldsValue(patch);
  };

  return (
    <Row gutter={12}>
      {/* 左侧：模板选择 + 生成表单 */}
      <Col xs={24} xl={10}>
        <Card size="small" title="变更模板" style={{ marginBottom: 12 }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {(tpls.data?.templates ?? []).map((t) => {
              const active = selectedTpl?.id === t.id;
              return (
                <div
                  key={t.id}
                  onClick={() => onTemplateSelect(t)}
                  style={{
                    cursor: 'pointer',
                    padding: '10px 12px',
                    border: `1px solid ${active ? '#1677ff' : '#f0f0f0'}`,
                    borderRadius: 8,
                    background: active ? '#e6f4ff' : '#fafafa',
                  }}
                >
                  <Space>
                    <Typography.Text strong>{t.name}</Typography.Text>
                    <StatusPill tone="neutral">{TARGET_LABELS[t.targetType] ?? t.targetType}</StatusPill>
                    <StatusPill tone="info">{t.steps.length} 步</StatusPill>
                  </Space>
                  <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 4 }}>{t.description}</div>
                </div>
              );
            })}
          </div>
        </Card>

        {selectedTpl && (
          <Card size="small" title={`生成方案：${selectedTpl.name}`} extra={<Button size="small" onClick={() => setSelectedTpl(null)}>返回</Button>}>
            <Form
              form={form}
              layout="vertical"
              initialValues={{ env: selectedTpl.defaultEnv }}
              onFinish={(v) => gen.mutate({ ...v })}
            >
              <Form.Item name="templateId" hidden>
                <Input />
              </Form.Item>
              <Form.Item name="targetType" hidden>
                <Input />
              </Form.Item>

              <Row gutter={8}>
                {(selectedTpl.targetType === 'middleware') && (
                  <>
                    <Col span={12}>
                      <Form.Item name="mwType" label="中间件类型" rules={[{ required: true, message: '必填' }]}>
                        <Input placeholder="mysql / redis / kafka" />
                      </Form.Item>
                    </Col>
                    <Col span={12}>
                      <Form.Item name="mwAddr" label="地址 host:port" rules={[{ required: true, message: '必填' }]}>
                        <Input placeholder="ehome-pro.mysql.ehomeinner.cn:13302" />
                      </Form.Item>
                    </Col>
                  </>
                )}
                {(selectedTpl.targetType === 'app' || selectedTpl.targetType === 'apollo') && (
                  <Col span={24}>
                    <Form.Item name="appId" label="目标应用 appId" rules={[{ required: true, message: '必填' }]}>
                      <Input placeholder="area / order-server ..." />
                    </Form.Item>
                  </Col>
                )}
                {selectedTpl.targetType === 'datacenter' && (
                  <Col span={24}>
                    <Form.Item label="说明">
                      <Alert type="info" showIcon message="机房迁移方案会自动取全量应用的影响面作为参考，不需指定具体应用" />
                    </Form.Item>
                  </Col>
                )}
              </Row>

              <Row gutter={8}>
                <Col span={12}>
                  <Form.Item name="dateToday" label="当前快照" extra="空=自动取最新">
                    <Select
                      allowClear
                      placeholder="自动"
                      options={(dates.data?.dates ?? []).map((d) => ({ value: d, label: d }))}
                    />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item name="datePrev" label="对比快照" extra="空=自动取上一个">
                    <Select
                      allowClear
                      placeholder="自动"
                      options={(dates.data?.dates ?? []).map((d) => ({ value: d, label: d }))}
                    />
                  </Form.Item>
                </Col>
              </Row>

              <Form.Item name="env" label="环境">
                <Segmented options={['PRO', 'SIT', 'DEV']} />
              </Form.Item>

              <Form.Item name="notes" label="备注（可选）">
                <Input.TextArea rows={2} placeholder="变更原因或特殊说明" />
              </Form.Item>

              <Button type="primary" htmlType="submit" block icon={<PlusOutlined />} loading={gen.isPending}>
                生成方案 + 风险评估
              </Button>
            </Form>
          </Card>
        )}
      </Col>

      {/* 右侧：方案列表 + 详情 */}
      <Col xs={24} xl={14}>
        <Card size="small" title={`已生成方案（${plans.data?.total ?? 0}）`}>
          <Table
            size="small"
            rowKey="id"
            loading={plans.isLoading}
            dataSource={plans.data?.plans ?? []}
            locale={{ emptyText: selectedTpl ? '生成一个试试吧' : '先选一个模板' }}
            pagination={{ pageSize: 8, size: 'small' }}
            columns={[
              {
                title: '方案',
                ellipsis: true,
                render: (_, r) => (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>{r.templateName}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {r.target.mwType ? `${r.target.mwType}:${r.target.mwAddr}` : r.target.appId ?? r.target.targetType}
                    </Typography.Text>
                  </Space>
                ),
              },
              {
                title: '风险',
                width: 110,
                render: (_, r) => {
                  const cfg = LEVEL_CONFIG[r.risk.level] ?? LEVEL_CONFIG.low;
                  return (
                    <Space direction="vertical" size={0}>
                      <StatusPill tone={cfg.tone} size="sm" dot>{cfg.label}</StatusPill>
                      <span style={{ fontSize: 11, color: '#8c8c8c' }}>评分 {(r.risk.score * 100).toFixed(0)}%</span>
                    </Space>
                  );
                },
              },
              {
                title: '爆炸半径',
                width: 90,
                dataIndex: ['risk', 'explosionApps'],
                render: (v: number) => v > 0 ? `${v} 应用` : '-',
              },
              {
                title: '创建时间',
                width: 150,
                dataIndex: 'createdAt',
                render: (v: string) => new Date(v).toLocaleString(),
              },
              {
                title: '操作',
                width: 80,
                render: (_, r) => (
                  <Button size="small" type="link" onClick={() => setViewing(r)}>详情</Button>
                ),
              },
            ]}
          />
        </Card>
      </Col>

      {/* 详情 Drawer */}
      <Drawer
        width={680}
        open={!!viewing}
        onClose={() => setViewing(null)}
        title={
          viewing && (
            <Space>
              <span>{viewing.templateName}</span>
              <StatusPill tone="neutral">{viewing.id}</StatusPill>
            </Space>
          )
        }
      >
        {viewing && (
          <>
            {/* 风险评估卡 */}
            <Card
              size="small"
              title={<Space><WarningOutlined /><span>风险评估</span></Space>}
              style={{ marginBottom: 12, background: LEVEL_CONFIG[viewing.risk.level]?.bg ?? '#fafafa' }}
            >
              <Row gutter={16}>
                <Col span={8}>
                  <Space direction="vertical" size={4}>
                    <Progress
                      type="circle"
                      size={72}
                      percent={Math.round(viewing.risk.score * 100)}
                      status={viewing.risk.level === 'critical' ? 'exception' : viewing.risk.level === 'high' ? 'active' : 'success'}
                    />
                    <StatusPill tone={LEVEL_CONFIG[viewing.risk.level]?.tone} dot>{LEVEL_CONFIG[viewing.risk.level]?.label}</StatusPill>
                  </Space>
                </Col>
                <Col span={16}>
                  <Descriptions size="small" column={2} bordered>
                    <Descriptions.Item label="爆炸半径">{viewing.risk.explosionApps} 个应用</Descriptions.Item>
                    <Descriptions.Item label="快照漂移">
                      {(viewing.risk.driftRatio * 100).toFixed(0)}%
                    </Descriptions.Item>
                    <Descriptions.Item label="近 30 天故障">{viewing.risk.recentFaults} 次</Descriptions.Item>
                    <Descriptions.Item label="模板">{viewing.templateName}</Descriptions.Item>
                  </Descriptions>
                </Col>
              </Row>
              {(viewing.risk.blockers ?? []).length > 0 && (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginTop: 12 }}
                  message="风险阻断条件"
                  description={
                    <ul style={{ margin: 0, paddingLeft: 20 }}>
                      {(viewing.risk.blockers ?? []).map((b, i) => <li key={i}>{b}</li>)}
                    </ul>
                  }
                />
              )}
              {viewing.risk.notes && (
                <Alert type="success" showIcon style={{ marginTop: 8 }} message={viewing.risk.notes} />
              )}
            </Card>

            {/* 变更目标 */}
            <Card size="small" title="变更目标" style={{ marginBottom: 12 }}>
              <Descriptions size="small" column={2} bordered>
                <Descriptions.Item label="模板">{viewing.templateName}</Descriptions.Item>
                <Descriptions.Item label="目标类型">{TARGET_LABELS[viewing.target.targetType] ?? viewing.target.targetType}</Descriptions.Item>
                {viewing.target.appId && <Descriptions.Item label="应用 appId">{viewing.target.appId}</Descriptions.Item>}
                {viewing.target.mwType && <Descriptions.Item label="中间件">{`${viewing.target.mwType}:${viewing.target.mwAddr}`}</Descriptions.Item>}
                <Descriptions.Item label="环境">{viewing.target.env}</Descriptions.Item>
                <Descriptions.Item label="快照对比">{`${viewing.target.datePrev ?? '自动'} → ${viewing.target.dateToday ?? '自动'}`}</Descriptions.Item>
                {viewing.target.notes && <Descriptions.Item label="备注" span={2}>{viewing.target.notes}</Descriptions.Item>}
              </Descriptions>
            </Card>

            {/* 执行步骤 */}
            <Card size="small" title="执行步骤与回退预案" style={{ marginBottom: 12 }}>
              <Table
                size="small"
                rowKey="no"
                pagination={false}
                dataSource={viewing.steps ?? []}
                columns={[
                  { title: '步骤', dataIndex: 'no', width: 60 },
                  { title: '标题', dataIndex: 'title', width: 130 },
                  { title: '操作提示', dataIndex: 'actionHint', ellipsis: true },
                  { title: '回退预案', dataIndex: 'rollback', ellipsis: true },
                ]}
              />
            </Card>

            {/* 联动按钮 */}
            <Space>
              <Button
                type="primary"
                icon={<LinkOutlined />}
                onClick={() => {
                  const params = new URLSearchParams();
                  if (viewing.target.appId) params.set('apolloAppId', viewing.target.appId);
                  if (viewing.target.mwType) params.set('middlewareType', viewing.target.mwType);
                  navigate(`/op-topology?${params.toString()}`);
                }}
              >
                查看完整拓扑
              </Button>
              <Button
                icon={<LinkOutlined />}
                onClick={() => {
                  const params = new URLSearchParams();
                  if (viewing.target.appId) params.set('appId', viewing.target.appId);
                  if (viewing.target.mwType) { params.set('mwType', viewing.target.mwType); params.set('addr', viewing.target.mwAddr ?? ''); }
                  params.set('env', viewing.target.env);
                  navigate(`/diagnosis?${params.toString()}`);
                }}
              >
                从此目标发起排障
              </Button>
            </Space>

            {viewing.risk.level === 'critical' && (
              <Alert
                type="error"
                showIcon
                message="此方案风险等级为 critical，建议人工介入评估后再执行"
                style={{ marginTop: 12 }}
              />
            )}
          </>
        )}
      </Drawer>
    </Row>
  );
}
