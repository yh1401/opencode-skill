import { createContext, useContext, useLayoutEffect, useMemo, useState, type ReactNode } from 'react';
import { ConfigProvider, theme as antdTheme, type ThemeConfig } from 'antd';
import zhCN from 'antd/locale/zh_CN';

export type ThemeMode = 'light' | 'dark';

const STORAGE_KEY = 'aiops-theme';
const FONT_SANS =
  "-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', 'Helvetica Neue', 'Microsoft YaHei', sans-serif";

const LIGHT_THEME: ThemeConfig = {
  token: {
    colorPrimary: '#0071E3',
    colorInfo: '#0071E3',
    colorSuccess: '#248A3D',
    colorWarning: '#C77700',
    colorError: '#C41E1A',
    colorText: '#1D1D1F',
    colorTextSecondary: '#6E6E73',
    colorTextTertiary: '#86868B',
    colorTextQuaternary: '#AEAEB2',
    colorBgLayout: '#F5F5F7',
    colorBgContainer: '#FFFFFF',
    colorBorder: '#D2D2D7',
    colorBorderSecondary: '#E5E5EA',
    borderRadius: 8,
    borderRadiusLG: 12,
    fontSize: 14,
    fontFamily: FONT_SANS,
    controlHeight: 34,
    wireframe: false,
    boxShadow: '0 1px 2px rgba(0,0,0,0.04)',
    boxShadowSecondary: '0 8px 24px rgba(0,0,0,0.08)',
  },
  components: {
    Layout: { headerHeight: 56, headerBg: '#F5F5F7', headerPadding: '0 24px', bodyBg: '#F5F5F7' },
    Menu: {
      itemHeight: 36,
      itemBorderRadius: 8,
      itemSelectedBg: '#EAF3FF',
      itemSelectedColor: '#0071E3',
      itemHoverBg: 'rgba(0,0,0,0.03)',
      itemColor: '#1D1D1F',
      itemHoverColor: '#1D1D1F',
      subMenuItemBg: 'transparent',
      groupTitleColor: '#86868B',
      iconSize: 15,
    },
    Table: {
      headerBg: '#FAFAFC',
      headerColor: '#6E6E73',
      headerSplitColor: 'transparent',
      rowHoverBg: '#F5F7FA',
      borderColor: '#E5E5EA',
      cellPaddingBlock: 10,
      cellPaddingInline: 12,
    },
    Card: { borderRadiusLG: 12, paddingLG: 20, paddingSM: 16 },
    Segmented: { itemSelectedBg: '#FFFFFF', trackBg: '#F0F0F3' },
    Drawer: { paddingLG: 24 },
    Tag: { borderRadiusSM: 999 },
    Button: { primaryShadow: 'none', defaultShadow: 'none' },
  },
};

const DARK_THEME: ThemeConfig = {
  algorithm: antdTheme.darkAlgorithm,
  token: {
    colorPrimary: '#2997FF',
    colorInfo: '#2997FF',
    colorSuccess: '#4CC26B',
    colorWarning: '#FFB340',
    colorError: '#FF6961',
    colorText: '#F5F5F7',
    colorTextSecondary: '#A1A1A6',
    colorTextTertiary: '#8E8E93',
    colorTextQuaternary: '#636366',
    colorBgLayout: '#161617',
    colorBgContainer: '#1F1F21',
    colorBorder: '#3A3A3C',
    colorBorderSecondary: '#303032',
    borderRadius: 8,
    borderRadiusLG: 12,
    fontSize: 14,
    fontFamily: FONT_SANS,
    controlHeight: 34,
    wireframe: false,
    boxShadow: '0 1px 2px rgba(0,0,0,0.4)',
    boxShadowSecondary: '0 8px 24px rgba(0,0,0,0.5)',
  },
  components: {
    Layout: { headerHeight: 56, headerBg: '#161617', headerPadding: '0 24px', bodyBg: '#161617' },
    Menu: {
      itemHeight: 36,
      itemBorderRadius: 8,
      itemSelectedBg: '#0A2A44',
      itemSelectedColor: '#64B5FF',
      itemHoverBg: 'rgba(255,255,255,0.06)',
      itemColor: '#F5F5F7',
      itemHoverColor: '#FFFFFF',
      subMenuItemBg: 'transparent',
      groupTitleColor: '#8E8E93',
      iconSize: 15,
    },
    Table: {
      headerBg: '#242426',
      headerColor: '#A1A1A6',
      headerSplitColor: 'transparent',
      rowHoverBg: '#2A2A2D',
      borderColor: '#303032',
      cellPaddingBlock: 10,
      cellPaddingInline: 12,
    },
    Card: { borderRadiusLG: 12, paddingLG: 20, paddingSM: 16 },
    Segmented: { itemSelectedBg: '#3A3A3C', trackBg: '#242426' },
    Drawer: { paddingLG: 24 },
    Tag: { borderRadiusSM: 999 },
    Button: { primaryShadow: 'none', defaultShadow: 'none' },
  },
};

function readStoredMode(): ThemeMode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    // 隐私模式或存储被禁用时退回亮色
  }
  return 'light';
}

interface ThemeContextValue {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
}

const ThemeContext = createContext<ThemeContextValue>({ mode: 'light', setMode: () => {} });

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<ThemeMode>(readStoredMode);

  useLayoutEffect(() => {
    document.documentElement.dataset.theme = mode;
    document.documentElement.style.colorScheme = mode;
    try {
      localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // 存储不可用时仅当前会话生效
    }
  }, [mode]);

  const config = useMemo(() => (mode === 'dark' ? DARK_THEME : LIGHT_THEME), [mode]);
  const value = useMemo(() => ({ mode, setMode }), [mode]);

  return (
    <ThemeContext.Provider value={value}>
      <ConfigProvider locale={zhCN} theme={config}>
        {children}
      </ConfigProvider>
    </ThemeContext.Provider>
  );
}

export function useThemeMode() {
  return useContext(ThemeContext);
}
