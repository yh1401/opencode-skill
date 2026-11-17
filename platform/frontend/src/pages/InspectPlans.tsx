import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  App as AntdApp,
  Button,
  Card,
  Checkbox,
  Col,
  Descriptions,
  Drawer,
  Empty,
  InputNumber,
  Row,
  Segmented,
  Space,
  Table,
  Typography,
} from 'antd';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { PillTone } from '../components/StatusPill';
import type { InspectionTask, RuleDef, RuleOverride } from '../types';

const SEV_TONE: Record<string, PillTone> = { P0: 'risk', P1: 'warn', P2: 'info', P3: 'neutral' };

export default function InspectPlans() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const [editing, setEditing] = useState<InspectionTask | null>(null);

  const rules = useQuery({ queryKey: ['inspect-rules'], queryFn: api.inspectRules });
  const tasks = useQuery({ queryKey: ['inspect-tasks'], queryFn: api.inspectTasks });

  const saveTask = useMutation({
    mutationFn: (body: { id: string; data: Partial<InspectionTask> }) =>
      api.inspectUpdateTask(body.id, body.data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['inspect-tasks'] });
      message.success('巡检方案已保存');
      setEditing(null);
    },
    onError: (e: Error) => message.error(e.message),
  });

  const buildOverrideMap = (task: InspectionTask | null, all: RuleDef[]): Record<string, RuleOverride> => {
    const map: Record<string, RuleOverride> = {};
    for (const r of all) {
      const existing = task?.rules?.[r.code];
      map[r.code] = {
        enabled: existing?.enabled ?? true,
        params: existing?.params ?? Object.fromEntries(r.params.map((p) => [p.key, p.default])),
      };
    }
    return map;
  };

  return (
    <Row gutter={12}>
      {/* 左侧：内置规则库 */}
      <Col xs={24} xl={10}>
        <Card size="small" title={`内置规则库（${rules.data?.rules?.length ?? 0} 条）`}>
          {(rules.data?.rules ?? []).map((r) => (
            <div key={r.code} style={{ padding: '8px 4px', borderBottom: '1px solid #f0f0f0' }}>
              <Space>
                <Typography.Text strong>{r.code}</Typography.Text>
                <StatusPill tone="neutral">{r.name}</StatusPill>
                <StatusPill tone={SEV_TONE[r.severity]} dot>{r.severity}</StatusPill>
                <StatusPill tone="ok">内置</StatusPill>
              </Space>
              <div style={{ fontSize: 12, color: '#8c8c8c', marginTop: 2 }}>{r.description}</div>
              {r.params.length > 0 && (
                <div style={{ marginTop: 4 }}>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>可配置参数：</Typography.Text>
                  {r.params.map((p) => (
                    <span key={p.key} style={{ marginTop: 2, display: 'inline-block' }}>
                      <StatusPill tone="neutral">
                        {p.label} = {String(p.default)}
                        {p.min !== undefined && p.max !== undefined
                          ? ` (${p.min}-${p.max})`
                          : p.min !== undefined
                            ? ` (≥${p.min})`
                            : p.max !== undefined
                              ? ` (≤${p.max})`
                              : ''}
                      </StatusPill>
                    </span>
                  ))}
                </div>
              )}
            </div>
          ))}
          {rules.isLoading && <div style={{ textAlign: 'center', padding: 20 }}>加载中…</div>}
        </Card>
      </Col>

      {/* 右侧：巡检方案列表 + 编辑 */}
      <Col xs={24} xl={14}>
        <Card size="small" title={`巡检方案（${tasks.data?.total ?? 0}）`}>
          <Table
            size="small"
            rowKey="id"
            loading={tasks.isLoading}
            dataSource={tasks.data?.tasks ?? []}
            pagination={false}
            columns={[
              {
                title: '方案',
                render: (_, t) => (
                  <Space direction="vertical" size={0}>
                    <Typography.Text strong>{t.name}</Typography.Text>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t.scene}</Typography.Text>
                  </Space>
                ),
              },
              {
                title: '范围',
                render: (_, t) => (
                  <Space wrap size={[4, 4]}>
                    <StatusPill tone="info" size="sm">{t.scope.env}</StatusPill>
                    {t.scope.appIds?.slice(0, 2).map((a) => <StatusPill key={a} tone="neutral" size="sm">{a}</StatusPill>)}
                    {t.scope.appIds && t.scope.appIds.length > 2 && (
                      <StatusPill tone="neutral" size="sm">+{t.scope.appIds.length - 2}</StatusPill>
                    )}
                  </Space>
                ),
              },
              {
                title: '规则启用',
                width: 130,
                render: (_, t) => {
                  const all = rules.data?.rules ?? [];
                  const enabled = all.filter((r) => t.rules?.[r.code]?.enabled !== false).length;
                  return <span>{enabled}/{all.length}</span>;
                },
              },
              {
                title: '状态',
                width: 80,
                dataIndex: 'enabled',
                render: (v: boolean) => v ? <StatusPill tone="ok" size="sm" dot>启用</StatusPill> : <StatusPill tone="neutral" size="sm" dot>禁用</StatusPill>,
              },
              {
                title: '操作',
                width: 120,
                render: (_, t) => (
                  <Space>
                    <Button size="small" type="link" onClick={() => setEditing(t)}>配置规则</Button>
                  </Space>
                ),
              },
            ]}
          />
          {!tasks.isLoading && (tasks.data?.tasks?.length ?? 0) === 0 && (
            <Empty description="暂无巡检方案" />
          )}
        </Card>
      </Col>

      {/* 编辑 Drawer */}
      {editing && (() => {
        const allRules = rules.data?.rules ?? [];
        const [overrides, setOverrides] = useState<Record<string, RuleOverride>>(
          buildOverrideMap(editing, allRules),
        );
        const changed = useMemo(() => {
          for (const r of allRules) {
            const ov = overrides[r.code];
            const old = editing.rules?.[r.code];
            if (old?.enabled !== ov.enabled) return true;
            if (JSON.stringify(old?.params ?? {}) !== JSON.stringify(ov.params ?? {})) return true;
          }
          return false;
        }, [overrides, editing, allRules]);

        const updateParam = (code: string, paramKey: string, value: any) => {
          setOverrides((prev) => ({
            ...prev,
            [code]: { ...prev[code], params: { ...(prev[code].params ?? {}), [paramKey]: value } },
          }));
        };
        const toggleEnabled = (code: string, enabled: boolean) => {
          setOverrides((prev) => ({ ...prev, [code]: { ...prev[code], enabled } }));
        };

        return (
          <Drawer
            width={760}
            open={!!editing}
            onClose={() => setEditing(null)}
            title={`配置规则：${editing.name}`}
            extra={
              <Button
                type="primary"
                disabled={!changed}
                loading={saveTask.isPending}
                onClick={() => {
                  const rules: Record<string, RuleOverride> = {};
                  for (const r of allRules) {
                    rules[r.code] = overrides[r.code];
                  }
                  saveTask.mutate({ id: editing.id, data: { rules } });
                }}
              >
                保存配置
              </Button>
            }
          >
            <Card size="small" style={{ marginBottom: 12 }}>
              <Descriptions size="small" column={2} bordered>
                <Descriptions.Item label="名称">{editing.name}</Descriptions.Item>
                <Descriptions.Item label="场景">{editing.scene}</Descriptions.Item>
                <Descriptions.Item label="环境">{editing.scope.env}</Descriptions.Item>
                <Descriptions.Item label="启用">
                  <Segmented
                    options={['启用', '禁用']}
                    value={editing.enabled ? '启用' : '禁用'}
                    onChange={(v) => {
                      saveTask.mutate({ id: editing.id, data: { enabled: v === '启用' } });
                    }}
                  />
                </Descriptions.Item>
              </Descriptions>
            </Card>

            {allRules.map((r) => {
              const ov = overrides[r.code];
              return (
                <Card
                  key={r.code}
                  size="small"
                  style={{ marginBottom: 8, opacity: ov.enabled ? 1 : 0.55 }}
                  bodyStyle={{ padding: '10px 14px' }}
                  title={
                    <Space>
                      <Checkbox checked={ov.enabled} onChange={(e) => toggleEnabled(r.code, e.target.checked)}>
                        <StatusPill tone={SEV_TONE[r.severity]} dot>{r.severity}</StatusPill>
                        <Typography.Text strong>{r.code} {r.name}</Typography.Text>
                      </Checkbox>
                    </Space>
                  }
                >
                  <div style={{ fontSize: 12, color: '#8c8c8c', marginBottom: 8 }}>{r.description}</div>
                  {r.params.length > 0 && (
                    <Space wrap>
                      {r.params.map((p) => (
                        <div key={p.key}>
                          <Typography.Text style={{ fontSize: 12 }}>{p.label}：</Typography.Text>
                          {p.type === 'number' && (
                            <InputNumber
                              size="small"
                              min={p.min}
                              max={p.max}
                              value={ov.params?.[p.key] ?? p.default}
                              onChange={(v) => updateParam(r.code, p.key, v)}
                              style={{ width: 110 }}
                            />
                          )}
                        </div>
                      ))}
                    </Space>
                  )}
                  {r.params.length === 0 && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>无参数</Typography.Text>
                  )}
                </Card>
              );
            })}
          </Drawer>
        );
      })()}
    </Row>
  );
}
