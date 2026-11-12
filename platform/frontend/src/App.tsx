import { useEffect, useMemo, useState } from 'react';
import { Layout, Menu, Popover, Segmented } from 'antd';
import { SettingOutlined } from '@ant-design/icons';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { ALL_ITEMS, HOME, NAV, type NavStatus } from './nav';
import { useThemeMode, type ThemeMode } from './theme';

const { Sider, Header, Content } = Layout;

/** 菜单状态药丸（设计系统 v1.0）：done 不显示标签，缺省即正常 */
const STATUS_PILL: Record<NavStatus, { tone: string; text: string } | null> = {
  done: null,
  partial: { tone: 'warn', text: '部分' },
  planned: { tone: 'neutral', text: '规划' },
};

export default function App() {
  const navigate = useNavigate();
  const location = useLocation();
  const { mode, setMode } = useThemeMode();

  const flat = useMemo(() => [HOME, ...ALL_ITEMS], []);
  const selected =
    [...flat]
      .sort((a, b) => b.key.length - a.key.length)
      .find((i) => location.pathname === i.key || location.pathname.startsWith(`${i.key}/`))
      ?.key ?? HOME.key;

  const groupOf = (key: string) => NAV.find((g) => g.items.some((i) => i.key === key))?.key;
  const [openKeys, setOpenKeys] = useState<string[]>(() => {
    const g = groupOf(selected);
    return g ? [g] : [];
  });

  useEffect(() => {
    const g = groupOf(selected);
    if (g) setOpenKeys((prev) => (prev.includes(g) ? prev : [...prev, g]));
  }, [selected]);

  const menuItems = useMemo(
    () => [
      { key: HOME.key, icon: HOME.icon, label: HOME.label },
      ...NAV.map((g) => ({
        key: g.key,
        icon: g.icon,
        label: g.label,
        children: g.items.map((i) => {
          const pill = STATUS_PILL[i.status];
          return {
            key: i.key,
            className: `menu-item-${i.status}`,
            label: (
              <span className="menu-label">
                {i.label}
                {pill && (
                  <span className={`status-pill status-pill-${pill.tone} status-pill-sm`}>{pill.text}</span>
                )}
              </span>
            ),
          };
        }),
      })),
    ],
    [],
  );

  return (
    <Layout className="app-shell">
      <Sider width={232} theme="light" className="app-sider">
        <div className="brand">
          <span className="brand-mark">视联</span>
          <span className="brand-text">AI 运维平台</span>
        </div>
        <Menu
          className="app-menu"
          theme="light"
          mode="inline"
          selectedKeys={[selected]}
          openKeys={openKeys}
          onOpenChange={(keys) => setOpenKeys(keys as string[])}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
        <Popover
          trigger="click"
          placement="rightBottom"
          content={
            <div className="theme-panel">
              <div className="theme-panel-title">外观主题</div>
              <Segmented
                block
                value={mode}
                onChange={(value) => setMode(value as ThemeMode)}
                options={[
                  { label: '亮色', value: 'light' },
                  { label: '暗色', value: 'dark' },
                ]}
              />
              <div className="theme-panel-hint">设置仅保存在当前浏览器</div>
            </div>
          }
        >
          <button type="button" className="sider-settings">
            <SettingOutlined />
            <span>设置</span>
          </button>
        </Popover>
      </Sider>
      <Layout>
        <Header className="app-header">
          <div className="page-title">{flat.find((i) => i.key === selected)?.label}</div>
        </Header>
        <Content className="app-content">
          <Routes>
            <Route path="/" element={<Navigate to={HOME.key} replace />} />
            <Route path={HOME.key} element={HOME.element} />
            {ALL_ITEMS.map((i) => (
              <Route key={i.key} path={i.key} element={i.element} />
            ))}
            <Route path="*" element={<Navigate to={HOME.key} replace />} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  );
}
