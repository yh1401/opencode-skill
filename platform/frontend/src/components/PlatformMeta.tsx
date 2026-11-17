import { useQuery } from '@tanstack/react-query';
import { api } from '../api';

/**
 * 平台数据底座：读取 /api/meta（拓扑模式与快照）与 /api/mcp/probe（MCP 探活），
 * 单行展示；有服务掉线时整行转橙/红。
 */
export default function PlatformMeta() {
  const meta = useQuery({ queryKey: ['meta'], queryFn: api.meta, staleTime: 5 * 60 * 1000 });
  const probe = useQuery({ queryKey: ['probe'], queryFn: api.mcpProbe, refetchInterval: 30000 });

  const servers = probe.data?.servers ?? [];
  const reachable = servers.filter((s) => s.reachable).length;
  const latestDate = [...(meta.data?.topo.dates ?? [])].sort().pop();

  // 全部掉线红色，部分掉线橙色，全部在线沿用默认次要色
  const tone =
    servers.length && reachable === 0
      ? '#C41E1A'
      : servers.length && reachable < servers.length
        ? '#C77700'
        : undefined;

  const parts: string[] = [];
  if (meta.data) parts.push(`topo ${meta.data.topo.mode}（快照 ${latestDate ?? '无'}）`);
  if (probe.data) parts.push(`上游服务 ${reachable}/${servers.length} 在线`);
  if (parts.length === 0) parts.push('加载中…');

  return (
    <div style={{ fontSize: 12, color: tone ?? '#86868B', marginTop: 12 }}>
      平台数据底座：{parts.join(' · ')} · 日志分析
    </div>
  );
}
