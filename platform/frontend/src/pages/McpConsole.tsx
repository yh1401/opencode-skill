import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  Form,
  Input,
  Row,
  Select,
  Space,
  Table,
} from 'antd';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { MCPTool } from '../types';

export default function McpConsole() {
  const { message } = AntdApp.useApp();
  const qc = useQueryClient();
  const [form] = Form.useForm();
  const [server, setServer] = useState<string>('pinpoint');
  const [result, setResult] = useState<{ ok: boolean; latencyMs: number; body: unknown } | null>(
    null,
  );

  const probe = useQuery({ queryKey: ['probe'], queryFn: api.mcpProbe, refetchInterval: 15000 });
  const tools = useQuery({ queryKey: ['mcp-tools'], queryFn: api.mcpTools, refetchInterval: 30000 });
  const logs = useQuery({ queryKey: ['mcp-logs'], queryFn: () => api.mcpLogs(100) });

  const call = useMutation({
    mutationFn: (v: { tool: string; args?: string }) => {
      let args: Record<string, unknown> | undefined;
      if (v.args && v.args.trim()) args = JSON.parse(v.args);
      return api.mcpCall({ server, tool: v.tool, args });
    },
    onSuccess: (res) => {
      setResult({ ok: true, latencyMs: res.latencyMs, body: res.result });
      qc.invalidateQueries({ queryKey: ['mcp-logs'] });
    },
    onError: (e: Error) => {
      setResult({ ok: false, latencyMs: 0, body: e.message });
      qc.invalidateQueries({ queryKey: ['mcp-logs'] });
    },
  });

  const servers = useMemo(() => {
    const names = new Set<string>();
    (probe.data?.servers ?? []).forEach((s) => names.add(s.name));
    (tools.data?.tools ?? []).forEach((t) => names.add(t.server));
    return Array.from(names);
  }, [probe.data, tools.data]);

  const toolOptions = (tools.data?.tools ?? [])
    .filter((t: MCPTool) => t.server === server)
    .map((t: MCPTool) => ({
      value: t.name,
      label: `${t.name}${t.allowed ? '' : '（未放行）'}`,
      disabled: !t.allowed,
    }));

  const toolsOfServer = (tools.data?.tools ?? []).filter((t: MCPTool) => t.server === server);

  return (
    <>
      <Card size="small" title="上游服务探针" style={{ marginBottom: 12 }}>
        <Table
          size="small"
          rowKey="name"
          pagination={false}
          loading={probe.isLoading}
          dataSource={probe.data?.servers ?? []}
          columns={[
            { title: '服务', dataIndex: 'name', width: 120 },
            {
              title: '状态',
              dataIndex: 'reachable',
              width: 160,
              render: (v: boolean, r) =>
                v ? (
                  <StatusPill tone="ok" size="sm" dot>可达 · {r.tools}</StatusPill>
                ) : (
                  <StatusPill tone="risk" size="sm" dot>不可达</StatusPill>
                ),
            },
            { title: '耗时', dataIndex: 'latencyMs', width: 100, render: (v: number) => `${v}ms` },
            { title: '地址', dataIndex: 'url', render: (v: string) => <span className="mono">{v}</span> },
            { title: '错误', dataIndex: 'error', render: (v?: string) => <span className="mono">{v || '-'}</span> },
          ]}
        />
      </Card>

      <Row gutter={12}>
        <Col xs={24} xl={14}>
          <Card size="small" title="工具调用（黑名单拦截 + 审计落盘）" style={{ marginBottom: 12 }}>
            <Form form={form} layout="inline" onFinish={(v) => call.mutate(v)} style={{ marginBottom: 12 }}>
              <Form.Item>
                <Select
                  style={{ width: 140 }}
                  value={server}
                  onChange={(v) => {
                    setServer(v);
                    form.setFieldValue('tool', undefined);
                  }}
                  options={servers.map((s) => ({ value: s, label: s }))}
                />
              </Form.Item>
              <Form.Item name="tool" rules={[{ required: true, message: '选择工具' }]}>
                <Select
                  style={{ width: 260 }}
                  placeholder="工具"
                  showSearch
                  optionFilterProp="label"
                  options={toolOptions}
                />
              </Form.Item>
              <Form.Item name="args">
                <Input style={{ width: 260 }} placeholder='参数 JSON，如 {"query":"up"}' />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" loading={call.isPending}>
                  调用
                </Button>
              </Form.Item>
            </Form>

            {result && (
              <Alert
                type={result.ok ? 'success' : 'error'}
                showIcon
                message={
                  result.ok ? `调用成功 · ${result.latencyMs}ms` : `调用失败 · ${result.latencyMs}ms`
                }
                description={
                  <pre className="mono" style={{ margin: 0, maxHeight: 260, overflow: 'auto' }}>
                    {JSON.stringify(result.body, null, 2)}
                  </pre>
                }
              />
            )}

            <Card size="small" type="inner" title={`${server} 工具清单`} style={{ marginTop: 12 }}>
              <Table
                size="small"
                rowKey="name"
                pagination={{ pageSize: 8, size: 'small' }}
                dataSource={toolsOfServer}
                columns={[
                  { title: '工具', dataIndex: 'name', width: 220 },
                  { title: '说明', dataIndex: 'description', ellipsis: true },
                  {
                    title: '放行',
                    dataIndex: 'allowed',
                    width: 80,
                    render: (v: boolean) =>
                      v ? (
                        <StatusPill tone="ok" size="sm" dot>是</StatusPill>
                      ) : (
                        <StatusPill tone="risk" size="sm" dot>否</StatusPill>
                      ),
                  },
                ]}
              />
            </Card>
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card size="small" title="调用审计">
            <Table
              size="small"
              rowKey={(r) => `${r.server}-${r.tool}-${r.at}`}
              loading={logs.isLoading}
              dataSource={logs.data?.logs ?? []}
              pagination={{ pageSize: 10, size: 'small' }}
              expandable={{
                expandedRowRender: (r) => (
                  <pre className="mono" style={{ margin: 0 }}>
                    {JSON.stringify({ args: r.args, error: r.error }, null, 2)}
                  </pre>
                ),
              }}
              columns={[
                {
                  title: '时间',
                  dataIndex: 'at',
                  width: 150,
                  render: (v: string) => new Date(v).toLocaleTimeString(),
                },
                {
                  title: '调用',
                  render: (_, r) => (
                    <Space size={4}>
                      <StatusPill tone="neutral" size="sm">{r.server}</StatusPill>
                      <span className="mono">{r.tool}</span>
                    </Space>
                  ),
                },
                {
                  title: '结果',
                  dataIndex: 'ok',
                  width: 70,
                  render: (v: boolean) =>
                    v ? (
                      <StatusPill tone="ok" size="sm" dot>ok</StatusPill>
                    ) : (
                      <StatusPill tone="risk" size="sm" dot>fail</StatusPill>
                    ),
                },
                { title: '耗时', dataIndex: 'latencyMs', width: 80, render: (v: number) => `${v}ms` },
              ]}
            />
          </Card>
        </Col>
      </Row>
    </>
  );
}
