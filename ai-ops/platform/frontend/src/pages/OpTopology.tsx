/**
 * 运维关系拓扑：直接内嵌 tprel 原型 topology_dagre.html（完整能力，1:1 效果）。
 *
 * 原型文件放在 public/tprel/ 下，其内部通过平台适配层（HTML 头部的 fetch 适配脚本）
 * 把 /api/external/operationTopology/* 映射到平台 /api/topo/op/*，数据源与平台保持一致。
 * 如需修改拓扑交互，直接改 public/tprel/topology_dagre.html，不要在此处重新实现。
 *
 * 告警中心 → 拓扑钻取：/op-topology?host=&fp=，通过 URL 参数与 postMessage 双保险定位节点。
 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Alert, Button, message } from 'antd';

const TOPOLOGY_URL = '/tprel/topology_dagre.html';

export default function OpTopology() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const host = params.get('host') ?? '';
  const fp = params.get('fp') ?? '';
  // 排障工作台「查看完整拓扑」传入的聚焦应用；展开层开关一并透传
  const focusApp = params.get('apolloAppId') ?? '';
  const expandDepNode = params.get('expandDepNode') ?? '';
  const [reloadKey, setReloadKey] = useState(0);
  const iframeRef = useRef<HTMLIFrameElement>(null);

  // 拓扑 iframe 详情卡里的「从这里开排障」通过 postMessage 回到 SPA 内跳转，避免整页刷新
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== window.location.origin) return;
      const data = e.data as { type?: string; url?: string; runId?: string; host?: string; env?: string; risks?: number } | null;
      if (!data) return;
      if (data.type === 'aiops-open-diagnosis' && typeof data.url === 'string' && data.url.startsWith('/diagnosis')) {
        navigate(data.url);
        return;
      }
      if (data.type === 'aiops-audit-run') {
        const r = data.risks ?? 0;
        const host = data.host ?? '';
        const label = host ? `（${host}）` : '';
        message.success({
          content: `稽核已发起${label}：发现 ${r} 项风险`,
          duration: 3,
          onClick: () => navigate('/audit'),
        });
        navigate('/audit');
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [navigate]);

  const src = useMemo(() => {
    const q = new URLSearchParams();
    if (focusApp) q.set('focusApp', focusApp);
    if (host) q.set('focusHost', host);
    if (fp) q.set('fp', fp);
    if (expandDepNode) q.set('expandDepNode', expandDepNode);
    const qs = q.toString();
    return qs ? `${TOPOLOGY_URL}?${qs}` : TOPOLOGY_URL;
  }, [focusApp, host, fp, expandDepNode]);

  // iframe 加载完成后 postMessage 双保险（避免 URL 解析失败 / 刷新时丢参）
  const handleLoad = () => {
    if (!focusApp && !host) return;
    iframeRef.current?.contentWindow?.postMessage(
      { type: 'aiops-focus', app: focusApp, host, fingerprint: fp },
      window.location.origin,
    );
  };

  const showAll = () => {
    setParams({});
    setReloadKey((k) => k + 1);
  };

  return (
    <>
      {focusApp && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 8 }}
          message={`已聚焦 ${focusApp} 的节点拓扑（应用 + 关联中间件）`}
          description="关闭归聚视图可查看全量拓扑；点「显示全部」清除聚焦条件。"
          action={
            <Button size="small" onClick={showAll}>
              显示全部
            </Button>
          }
        />
      )}
      {!focusApp && host && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 8 }}
          message={`已定位到 ${host} 的告警节点`}
          action={
            <Button size="small" onClick={showAll}>
              显示全部
            </Button>
          }
        />
      )}
      <iframe
        key={reloadKey}
        ref={iframeRef}
        title="运维关系拓扑"
        src={src}
        onLoad={handleLoad}
        style={{
          width: '100%',
          height: focusApp || host ? 'calc(100vh - 152px)' : 'calc(100vh - 96px)',
          border: 'none',
          display: 'block',
          borderRadius: 12,
          background: '#fff',
        }}
      />
    </>
  );
}
