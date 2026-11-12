import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Collapse,
  Descriptions,
  Empty,
  Modal,
  Progress,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Typography,
  message,
} from 'antd';
import { DownloadOutlined, SyncOutlined } from '@ant-design/icons';
import { api } from '../api';
import StatusPill from '../components/StatusPill';
import type { OpSyncResult } from '../types';

const sizeText = (n?: number) => {
  if (!n && n !== 0) return '-';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(2)} MB`;
};

/**
 * 拓扑数据同步：tprel 采集链路控制台。
 * 手动触发全量同步（Apollo 实例 → 应用 → 配置解析 → 关系快照），
 * 进度实时轮询、结果总览（新增/消失关系、类型分布、无中间件应用）与同步日志查看。
 * file 快照只读模式下采集动作降级为提示。
 */
export default function TopoSync() {
  const qc = useQueryClient();
  const [logDate, setLogDate] = useState<string | undefined>();

  const meta = useQuery({ queryKey: ['meta'], queryFn: api.meta });
  const fileMode = meta.data?.topo?.mode !== 'http';

  const status = useQuery({
    queryKey: ['op-sync-status'],
    queryFn: api.opSyncStatus,
    refetchInterval: (q) => (q.state.data?.running ? 1500 : false),
  });
  const st = status.data;

  const logs = useQuery({
    queryKey: ['op-sync-logs', logDate],
    queryFn: () => api.opSyncLogs(logDate ? { date: logDate } : {}),
  });
  useEffect(() => {
    const files = logs.data?.files ?? [];
    if (!logDate && files.length > 0) setLogDate(files[0].date);
  }, [logs.data, logDate]);

  const dates = useQuery({ queryKey: ['dates'], queryFn: api.topoDates });

  const startSync = useMutation({
    mutationFn: api.opSync,
    onSuccess: (res) => {
      if (res.supported === false) {
        Modal.warning({ title: '当前为 file 快照只读模式', content: res.message });
        return;
      }
      message.success(res.started ? '同步任务已提交' : res.message || '已有任务在执行');
      qc.invalidateQueries({ queryKey: ['op-sync-status'] });
    },
    onError: (e) => message.error((e as Error).message),
  });

  const result = st?.result as OpSyncResult | undefined;
  const typeRows = useMemo(
    () =>
      Object.entries(result?.typeStats ?? {})
        .map(([type, count]) => ({ type, count }))
        .sort((a, b) => b.count - a.count),
    [result],
  );

  const running = !!st?.running;
  const percent = st?.percent ?? 0;

  return (
    <>
      {fileMode && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="当前平台以 TOPO_MODE=file 直读 topo 本地快照（只读）"
          description="手动采集与同步日志需部署侧以 TOPO_MODE=http 对接 topo 服务后可用；下方同步状态与日志接口会返回降级说明。快照由 topo 服务定时任务（cronTask）每日生成。"
        />
      )}

      <Row gutter={12}>
        <Col xs={24} md={8}>
          <Card size="small" title="手动同步" style={{ marginBottom: 12 }}>
            <p style={{ color: '#8c8c8c', fontSize: 12 }}>
              全量采集 Apollo 实例 → 应用 → namespace 配置解析 → 生成当日关系快照，为分钟级长任务，
              提交后可关闭本页，进度在任务状态区轮询展示。
            </p>
            <Button
              type="primary"
              icon={<SyncOutlined />}
              loading={startSync.isPending}
              disabled={running}
              onClick={() => startSync.mutate()}
            >
              {running ? '同步进行中' : '开始同步'}
            </Button>
            {st?.supported === false && (
              <span style={{ marginLeft: 8 }}>
                <StatusPill tone="warn" dot>
                  file 模式不支持
                </StatusPill>
              </span>
            )}
          </Card>
          <Card size="small" title="任务状态" style={{ marginBottom: 12 }}>
            {status.isLoading && <Spin />}
            {st && !st.running && !st.startTime && !st.supported && (
              <Empty description="file 只读模式，无采集任务" styles={{ image: { height: 48 } }} />
            )}
            {st && (st.startTime || st.running) && (
              <>
                {running && (
                  <Progress
                    percent={percent >= 0 ? percent : 0}
                    status={percent < 0 ? 'active' : undefined}
                    showInfo={percent >= 0}
                    style={{ marginBottom: 4 }}
                  />
                )}
                <Descriptions size="small" column={1}>
                  {running && st.stageText && (
                    <Descriptions.Item label="阶段">{st.stageText}</Descriptions.Item>
                  )}
                  <Descriptions.Item label="触发方式">{st.triggerBy || '-'}</Descriptions.Item>
                  <Descriptions.Item label="开始时间">{st.startTime || '-'}</Descriptions.Item>
                  {st.endTime && (
                    <Descriptions.Item label="结束时间">
                      {st.endTime}（{((st.costMs ?? 0) / 1000).toFixed(1)}s）
                    </Descriptions.Item>
                  )}
                </Descriptions>
                {st.stats && (
                  <Space wrap size={6} style={{ marginTop: 8 }}>
                    <StatusPill tone="info">实例 {st.stats.hosts}</StatusPill>
                    <StatusPill tone="info">应用 {st.stats.apps}</StatusPill>
                    <StatusPill tone="ok">中间件 {st.stats.middleware}</StatusPill>
                    <StatusPill tone="info">关系 {st.stats.relations}</StatusPill>
                  </Space>
                )}
                {st.error && <Alert type="error" showIcon style={{ marginTop: 8 }} message={st.error} />}
              </>
            )}
          </Card>
        </Col>
        <Col xs={24} md={16}>
          <Card size="small" title="最近一次同步结果" style={{ marginBottom: 12 }}>
            {!result && <Empty description="暂无同步结果" styles={{ image: { height: 48 } }} />}
            {result && (
              <>
                <Row gutter={8}>
                  <Col span={4}>
                    <Statistic title="入库关系" value={result.insertCount ?? 0} />
                  </Col>
                  <Col span={4}>
                    <Statistic title="新增" value={result.newCount ?? 0} valueStyle={{ color: '#3f8600' }} />
                  </Col>
                  <Col span={4}>
                    <Statistic title="消失" value={result.lostCount ?? 0} valueStyle={{ color: '#cf1322' }} />
                  </Col>
                  <Col span={4}>
                    <Statistic title="总请求" value={result.requestCount ?? 0} />
                  </Col>
                  <Col span={4}>
                    <Statistic title="耗时(s)" value={((result.costMs ?? 0) / 1000).toFixed(1)} />
                  </Col>
                  <Col span={4}>
                    <Statistic title="对比快照" value={result.prevDate || '首次'} precision={0} />
                  </Col>
                </Row>
                <Space wrap size={6} style={{ marginTop: 8 }}>
                  <StatusPill tone={result.partial ? 'warn' : 'ok'} dot>
                    {result.resultText || (result.partial ? '部分完成' : '成功')}
                  </StatusPill>
                  <StatusPill tone="neutral">触发：{result.triggerBy || '-'}</StatusPill>
                  {result.relationRows !== undefined && (
                    <StatusPill tone={result.relationErr ? 'risk' : 'neutral'} dot>
                      跳①关系 {result.relationRows} 行 / {result.relationWrote ? '已写入' : '跳过写入'}
                      {result.relationMsg ? `（${result.relationMsg}）` : ''}
                    </StatusPill>
                  )}
                </Space>
                {(result.typeStats ?? undefined) && (
                  <Table
                    size="small"
                    style={{ marginTop: 12 }}
                    rowKey="type"
                    pagination={false}
                    dataSource={typeRows}
                    columns={[
                      { title: '中间件类型', dataIndex: 'type' },
                      { title: '关系数', dataIndex: 'count' },
                    ]}
                  />
                )}
                <Collapse
                  size="small"
                  style={{ marginTop: 12 }}
                  items={[
                    (result.newRelations?.length ?? 0) > 0 && {
                      key: 'new',
                      label: `新增关系明细（${result.newRelations?.length}）`,
                      children: (
                        <pre className="log-pre">{result.newRelations?.slice(0, 100).join('\n')}</pre>
                      ),
                    },
                    (result.lostRelations?.length ?? 0) > 0 && {
                      key: 'lost',
                      label: `消失关系明细（${result.lostRelations?.length}）`,
                      children: (
                        <pre className="log-pre">{result.lostRelations?.slice(0, 100).join('\n')}</pre>
                      ),
                    },
                    (result.noMiddlewareCount ?? 0) > 0 && {
                      key: 'nomw',
                      label: `无中间件应用（${result.noMiddlewareCount}）`,
                      children: (
                        <Table
                          size="small"
                          rowKey="appId"
                          pagination={{ pageSize: 8, size: 'small' }}
                          dataSource={result.noMiddlewareApps ?? []}
                          columns={[
                            { title: 'appId', dataIndex: 'appId' },
                            { title: '应用名称', dataIndex: 'appName', render: (v: string) => v || '-' },
                          ]}
                        />
                      ),
                    },
                    (result.unfinishedApps?.length ?? 0) > 0 && {
                      key: 'unfinished',
                      label: `未完成应用（${result.unfinishedApps?.length}）`,
                      children: (
                        <pre className="log-pre">{result.unfinishedApps?.slice(0, 100).join('\n')}</pre>
                      ),
                    },
                  ].filter(Boolean) as any[]}
                />
              </>
            )}
          </Card>
        </Col>
      </Row>

      <Row gutter={12}>
        <Col xs={24} md={14}>
          <Card size="small" title="同步日志" style={{ marginBottom: 12 }}>
            {logs.data?.supported === false && (
              <Alert type="info" showIcon message={logs.data.message || 'file 模式无采集日志'} />
            )}
            {logs.data?.supported !== false && (
              <>
                <Space wrap style={{ marginBottom: 8 }}>
                  <Select
                    style={{ width: 160 }}
                    placeholder="日志日期"
                    value={logDate}
                    onChange={setLogDate}
                    options={(logs.data?.files ?? []).map((f) => ({
                      value: f.date,
                      label: `${f.date}（${sizeText(f.sizeBytes)}）`,
                    }))}
                  />
                  <a href={`/api/topo/op/sync/logs?${logDate ? `date=${logDate}&` : ''}download=1`}>
                    <Button size="small" icon={<DownloadOutlined />}>
                      下载完整日志
                    </Button>
                  </a>
                  {logs.data?.truncated && (
                    <StatusPill tone="warn">仅展示尾部 256KB</StatusPill>
                  )}
                </Space>
                <pre className="log-pre">
                  {logs.data?.content || (logs.isLoading ? '加载中…' : '暂无日志内容')}
                </pre>
              </>
            )}
          </Card>
        </Col>
        <Col xs={24} md={10}>
          <Card size="small" title="本地快照（file 数据源）">
            <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
              快照目录由 topo 服务每日定时写入，平台 file 模式直读。历史日期可用于「资源拓扑与影响面」的回放与 Diff。
            </Typography.Paragraph>
            <Table
              size="small"
              rowKey="d"
              pagination={false}
              dataSource={(dates.data?.dates ?? []).map((d) => ({ d }))}
              columns={[{ title: '快照日期', dataIndex: 'd' }]}
            />
          </Card>
        </Col>
      </Row>
    </>
  );
}
