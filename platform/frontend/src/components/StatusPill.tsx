import type { ReactNode } from 'react';

/** 状态药丸语义（设计系统 v1.0）：浅底 + 深字 + 可选圆点 */
export type PillTone = 'ok' | 'risk' | 'warn' | 'info' | 'neutral';

interface Props {
  tone?: PillTone;
  size?: 'md' | 'sm';
  dot?: boolean;
  children: ReactNode;
}

/**
 * 全站统一的状态标签，替代 AntD `<Tag color="red|green|...">` 的高饱和实底写法。
 * 颜色见 src/styles.css 的 .status-pill-* 类。
 */
export default function StatusPill({ tone = 'neutral', size = 'md', dot = false, children }: Props) {
  return (
    <span className={`status-pill status-pill-${tone}${size === 'sm' ? ' status-pill-sm' : ''}`}>
      {dot && <span className={`status-dot status-dot-${tone}`} />}
      {children}
    </span>
  );
}
