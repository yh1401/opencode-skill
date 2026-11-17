import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Result, Space, Typography } from 'antd';
import { ExportOutlined, ReloadOutlined } from '@ant-design/icons';
import { api } from '../api';
import StatusPill from '../components/StatusPill';

/**
 * 日志分析服务地址（独立的 log_analyzer FastAPI 服务）。
 * 优先取构建期变量 VITE_LOG_ANALYZER_URL；未配置时按当前访问主机推导 :8000，
 * 这样 Docker 部署（浏览器开 127.0.0.1:5173）也能指向宿主机上的服务。
 */
function resolveAnalyzerURL(): string {
  const fromEnv = (import.meta.env.VITE_LOG_ANALYZER_URL as string | undefined)?.trim();
  if (fromEnv) return fromEnv.replace(/\/+$/, '');
  if (typeof window !== 'undefined') {
    return `${window.location.protocol}//${window.location.hostname}:8000`;
  }
  return 'http://127.0.0.1:8000';
}

const LOG_ANALYZER_URL = resolveAnalyzerURL();

export default function LogAnalysis() {
  const [reloadKey, setReloadKey] = useState(0);

  // 由后端探测，避免浏览器跨域；不可用时每 15s 自动重试，服务起来后自动切换为嵌入模式
  const health = useQuery({
    queryKey: ['log-analyzer-health'],
    queryFn: api.logAnalyzerHealth,
    retry: false,
    refetchInterval: (q) => (q.state.data?.reachable ? false : 15000),
  });

  const checking = health.isFetching && !health.data;
  const reachable = health.data?.reachable ?? false;
  const tone = checking ? 'neutral' : reachable ? 'ok' : 'warn';
  const toneText = checking ? '检测中…' : reachable ? '可用' : '不可用';

  return (
    <Space direction="vertical" size={12} style={{ display: 'flex' }}>
      <Card size="small">
        <Space wrap>
          <Typography.Text type="secondary">服务地址</Typography.Text>
          <StatusPill tone="info">{LOG_ANALYZER_URL}</StatusPill>
          <Typography.Text type="secondary">状态</Typography.Text>
          <StatusPill tone={tone} dot>{toneText}</StatusPill>
          {reachable && health.data && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              HTTP {health.data.status} · {health.data.latencyMs}ms
            </Typography.Text>
          )}
          <Button size="small" icon={<ReloadOutlined />} loading={health.isFetching} onClick={() => health.refetch()}>
            重新检测
          </Button>
          <Button
            size="small"
            icon={<ReloadOutlined />}
            disabled={!reachable}
            onClick={() => setReloadKey((k) => k + 1)}
          >
            重新加载
          </Button>
          <Button
            size="small"
            type="link"
            icon={<ExportOutlined />}
            href={LOG_ANALYZER_URL}
            target="_blank"
            rel="noreferrer"
            disabled={!reachable}
          >
            新窗口打开
          </Button>
        </Space>
      </Card>

      {reachable ? (
        <>
          <Alert
            type="info"
            showIcon
            message="日志分析由独立的 log_analyzer 服务提供，本页以 iframe 方式接入；排障证据链的日志取证优先走 ES，服务不可用时该步自动跳过。"
          />
          <Card size="small" styles={{ body: { padding: 0 } }}>
            <iframe
              key={reloadKey}
              title="日志分析"
              src={LOG_ANALYZER_URL}
              style={{ width: '100%', height: 'calc(100vh - 260px)', border: 'none', display: 'block' }}
            />
          </Card>
        </>
      ) : (
        <Card size="small">
          <Result
            status="warning"
            title="日志分析服务未就绪"
            subTitle={
              health.data?.hint ||
              (health.isError
                ? '无法获取服务状态，请确认平台后端可用后重试'
                : '正在探测 log_analyzer 服务…')
            }
          >
            <Space direction="vertical" size={8} style={{ display: 'flex', textAlign: 'left' }}>
              <Typography.Text type="secondary">
                该页访问的是独立的 log_analyzer（FastAPI）服务，不属于平台四路上游 MCP。平台其他功能不依赖它：
                排障日志证据优先走 ES，服务不可用时该步自动 <Typography.Text code>skipped</Typography.Text>。
              </Typography.Text>

              {health.data && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  探测详情：HTTP {health.data.status || '—'} · {health.data.latencyMs}ms
                  {health.data.detail ? ` · 响应：${health.data.detail}` : ''}
                </Typography.Text>
              )}

              <Typography.Text strong>启动服务（源码目录按实际位置调整）</Typography.Text>
              <Typography.Paragraph
                code
                copyable
                style={{ fontSize: 12, marginBottom: 0, whiteSpace: 'pre-wrap' }}
              >
                {`cd log/log_analyzer\nPYTHONPATH=. venv/bin/python -m uvicorn log_analyzer.web.app:app --port 8000`}
              </Typography.Paragraph>

              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                服务不在 :8000 时：构建前端前设置 <Typography.Text code>VITE_LOG_ANALYZER_URL=http://host:port</Typography.Text>
                （本页嵌入地址），并同步设置后端环境变量 <Typography.Text code>LOG_ANALYZER_URL</Typography.Text>
                （健康探测与排障取证共用）。
              </Typography.Text>
            </Space>
          </Result>
        </Card>
      )}
    </Space>
  );
}
