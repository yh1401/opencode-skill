import { useEffect, useMemo, useState } from 'react';
import { Button, Layout, Menu, Popover, Segmented, Switch } from 'antd';
import { MenuFoldOutlined, MenuOutlined, MenuUnfoldOutlined, SettingOutlined } from '@ant-design/icons';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { ALL_ITEMS, HOME, NAV, type NavGroup, type NavStatus } from './nav';
import { useThemeMode, type ThemeMode } from './theme';

const { Sider, Header, Content } = Layout;

/** 菜单状态药丸（设计系统 v1.0）：done 不显示标签，缺省即正常 */
const STATUS_PILL: Record<NavStatus, { tone: string; text: string } | null> = {
  done: null,
  partial: { tone: 'warn', text: '部分' },
  planned: { tone: 'neutral', text: '规划' },
};

/** 菜单显示偏好：默认收起规划中入口、模型/算法整组与运营指标（评价体系），割接维护按需隐藏 */
interface MenuPrefs {
  planned: boolean;
  change: boolean;
  ai: boolean;
  outcome: boolean;
}

const MENU_PREFS_KEY = 'aiops-menu-prefs';
const DEFAULT_PREFS: MenuPrefs = { planned: false, change: true, ai: false, outcome: false };

function readPrefs(): MenuPrefs {
  try {
    const raw = localStorage.getItem(MENU_PREFS_KEY);
    if (raw) return { ...DEFAULT_PREFS, ...(JSON.parse(raw) as Partial<MenuPrefs>) };
  } catch {
    // 隐私模式或存储被禁用时退回默认
  }
  return DEFAULT_PREFS;
}

/** 按偏好过滤菜单：整组开关优先，其次过滤规划项；空组直接移除 */
function visibleNav(prefs: MenuPrefs): NavGroup[] {
  return NAV.map((g) => {
    if (g.toggle && !prefs[g.toggle]) return { ...g, items: [] };
    const items = g.items.filter(
      (i) => i.status !== 'planned' || prefs.planned || g.plannedGroup,
    );
    return { ...g, items };
  }).filter((g) => g.items.length > 0);
}

export default function App() {
  const navigate = useNavigate();
  const location = useLocation();
  const { mode, setMode } = useThemeMode();

  const [prefs, setPrefs] = useState<MenuPrefs>(readPrefs);
  useEffect(() => {
    try {
      localStorage.setItem(MENU_PREFS_KEY, JSON.stringify(prefs));
    } catch {
      // 存储不可用时仅当前会话生效
    }
  }, [prefs]);

  const nav = useMemo(() => visibleNav(prefs), [prefs]);
  const hiddenCount = useMemo(
    () => ALL_ITEMS.length - nav.reduce((n, g) => n + g.items.length, 0),
    [nav],
  );

  const flat = useMemo(() => [HOME, ...ALL_ITEMS], []);
  const selected =
    [...flat]
      .sort((a, b) => b.key.length - a.key.length)
      .find((i) => location.pathname === i.key || location.pathname.startsWith(`${i.key}/`))
      ?.key ?? HOME.key;

  const groupOf = (key: string) => nav.find((g) => g.items.some((i) => i.key === key))?.key;
  const [openKeys, setOpenKeys] = useState<string[]>(() => {
    const g = groupOf(selected);
    return g ? [g] : [];
  });

  useEffect(() => {
    const g = groupOf(selected);
    if (g) setOpenKeys((prev) => (prev.includes(g) ? prev : [...prev, g]));
  }, [selected, nav]);

  const menuItems = useMemo(
    () => [
      { key: HOME.key, icon: HOME.icon, label: HOME.label },
      ...nav.map((g) => ({
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
    [nav],
  );

  const toggle = (key: keyof MenuPrefs) => (checked: boolean) =>
    setPrefs((prev) => ({ ...prev, [key]: checked }));

  // 一键展开 / 合并：所有可见分组都展开时视为「已展开」
  const allOpen = nav.length > 0 && nav.every((g) => openKeys.includes(g.key));
  const toggleAllGroups = () => setOpenKeys(allOpen ? [] : nav.map((g) => g.key));

  const menuPanel = (
    <div className="menu-panel">
      <div className="theme-panel-title">菜单显示</div>
      <div className="menu-panel-row">
        <div>
          <div className="menu-panel-label">规划中功能</div>
          <div className="menu-panel-desc">各场景里尚未上线的占位入口</div>
        </div>
        <Switch size="small" checked={prefs.planned} onChange={toggle('planned')} />
      </div>
      <div className="menu-panel-row">
        <div>
          <div className="menu-panel-label">模型与算法</div>
          <div className="menu-panel-desc">样本 / 特征 / 预处理 / 算法 / 训练 / 模型库</div>
        </div>
        <Switch size="small" checked={prefs.ai} onChange={toggle('ai')} />
      </div>
      <div className="menu-panel-row">
        <div>
          <div className="menu-panel-label">智能割接维护</div>
          <div className="menu-panel-desc">变更方案 / 风险评估 / 执行与验证</div>
        </div>
        <Switch size="small" checked={prefs.change} onChange={toggle('change')} />
      </div>
      <div className="menu-panel-row">
        <div>
          <div className="menu-panel-label">运营指标与评价体系</div>
          <div className="menu-panel-desc">考核指标看板（时延 / 达标率 / 自动化率）</div>
        </div>
        <Switch size="small" checked={prefs.outcome} onChange={toggle('outcome')} />
      </div>
      <div className="theme-panel-hint">
        {hiddenCount > 0 ? `已隐藏 ${hiddenCount} 项，仍可通过地址直接访问` : '全部菜单已显示'}
        <br />
        设置仅保存在当前浏览器
      </div>
      {hiddenCount > 0 && (
        <Button
          size="small"
          type="link"
          className="menu-panel-reset"
          onClick={() => setPrefs({ planned: true, change: true, ai: true, outcome: true })}
        >
          全部显示
        </Button>
      )}
    </div>
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
        <div className="sider-actions">
          <button type="button" className="sider-settings" onClick={toggleAllGroups}>
            {allOpen ? <MenuFoldOutlined /> : <MenuUnfoldOutlined />}
            <span>{allOpen ? '合并全部' : '展开全部'}</span>
          </button>
          <Popover trigger="click" placement="rightBottom" content={menuPanel}>
            <button type="button" className="sider-settings">
              <MenuOutlined />
              <span>菜单显示</span>
              {hiddenCount > 0 && <span className="sider-settings-count">{hiddenCount}</span>}
            </button>
          </Popover>
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
        </div>
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
