import type { ReactNode } from 'react';
import {
  AlertOutlined,
  ApartmentOutlined,
  ApiOutlined,
  BugOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DeploymentUnitOutlined,
  ExperimentOutlined,
  LineChartOutlined,
  RobotOutlined,
  SwapOutlined,
  TrophyOutlined,
} from '@ant-design/icons';
import Dashboard from './pages/Dashboard';
import Topology from './pages/Topology';
import OpTopology from './pages/OpTopology';
import TopoSync from './pages/TopoSync';
import ChangePlans from './pages/ChangePlans';
import Inspection from './pages/Inspection';
import InspectPlans from './pages/InspectPlans';
import Diagnosis from './pages/Diagnosis';
import McpConsole from './pages/McpConsole';
import Tickets from './pages/Tickets';
import Audit from './pages/Audit';
import LogAnalysis from './pages/LogAnalysis';
import Placeholder from './pages/Placeholder';
import AlarmCenter from './pages/AlarmCenter';
import Metrics from './pages/Metrics';
import Degradation from './pages/Degradation';
import BizMonitor from './pages/BizMonitor';
import Quality from './pages/Quality';
import Relations from './pages/Relations';
import Knowledge from './pages/Knowledge';

export type NavStatus = 'done' | 'partial' | 'planned';

/** 菜单显示开关：绑定分组的显隐，偏好保存在浏览器本地 */
export type NavToggle = 'change' | 'ai' | 'outcome';

export interface NavItem {
  key: string;
  label: string;
  element: ReactNode;
  status: NavStatus;
}

export interface NavGroup {
  key: string;
  label: string;
  icon: ReactNode;
  /** 绑定到「菜单显示」里的开关；不填则常驻 */
  toggle?: NavToggle;
  /** 整组均为规划能力：开关打开时整组显示，不受「规划中功能」过滤 */
  plannedGroup?: boolean;
  items: NavItem[];
}

export const HOME = {
  key: '/dashboard',
  label: '总览',
  icon: <DashboardOutlined />,
  element: <Dashboard />,
} as const;

/**
 * 菜单结构对齐《附件4.1 计划任务书》：
 * 5 大维护场景（监控排障 / 巡检 / 割接维护 / 云网数据治理 / 网络及服务质量优化）
 * + 运营指标 + 通用能力 + 模型与算法。
 *
 * 排布原则（按日常使用顺序）：
 * 1. 场景组内按处置动线排序：告警发现 → 排障定位 → 日志下钻 → 观测 → 工单闭环；
 *    规划中入口统一沉到组尾，避免打断主流程。
 * 2. 已落地的核心页面靠上，部分/规划页靠下；通用能力只保留当下可用的接口、
 *    知识库与平台服务。
 * 3. 模型与算法（样本 / 特征 / 预处理 / 算法 / 训练 / 模型）整组为规划能力，
 *    割接维护与模型与算法两组可在「菜单显示」面板中整组隐藏，菜单保持精简。
 * 未实现项以占位页呈现，保留入口与规划口径。
 */
export const NAV: NavGroup[] = [
  {
    key: 'monitor',
    label: '智能监控排障',
    icon: <BugOutlined />,
    items: [
      {
        key: '/alert-center',
        label: '告警中心',
        status: 'done',
        element: <AlarmCenter />,
      },
      { key: '/diagnosis', label: '排障工作台', status: 'done', element: <Diagnosis /> },
      { key: '/log-analysis', label: '日志分析', status: 'done', element: <LogAnalysis /> },
      {
        key: '/biz-monitor',
        label: '外部业务接口监控',
        status: 'done',
        element: <BizMonitor />,
      },
      {
        key: '/tickets',
        label: '隐患与工单',
        status: 'done',
        element: <Tickets />,
      },
      {
        key: '/diag-flows',
        label: '排障流程管理',
        status: 'planned',
        element: (
          <Placeholder
            title="排障流程管理"
            source="任务书 · 功能清单（智能排障 / 流程管理）"
            status="planned"
            summary="当前排障为固定四步流水线（拓扑影响面 → 证据采集 → 规则打分），尚不支持流程定义与节点编排。"
            foundation={['四步排障流程（硬编码）', 'SSE 增量证据链', '步骤落盘与历史会话']}
            items={[
              '排障流程模板定义与版本管理',
              '节点编排（拓扑 / 指标 / 日志 / 脚本 / 大模型分析）',
              '条件分支与人工介入节点',
              '流程执行实例追踪与耗时统计',
              '按场景绑定不同排障流程',
            ]}
            related={{ label: '去排障工作台', to: '/diagnosis' }}
          />
        ),
      },
    ],
  },
  {
    key: 'inspection',
    label: '智能巡检',
    icon: <ExperimentOutlined />,
    items: [
      {
        key: '/inspection',
        label: '巡检任务与报告',
        status: 'partial',
        element: <Inspection />,
      },
      {
        key: '/inspect-plans',
        label: '巡检方案与脚本',
        status: 'done',
        element: <InspectPlans />,
      },
    ],
  },
  {
    key: 'change',
    label: '智能割接维护',
    icon: <SwapOutlined />,
    toggle: 'change',
    items: [
      {
        key: '/change/plans',
        label: '变更方案',
        status: 'done',
        element: <ChangePlans />,
      },
      {
        key: '/change/risk',
        label: '风险评估',
        status: 'done',
        element: <ChangePlans />,
      },
      {
        key: '/change/execution',
        label: '执行与验证',
        status: 'planned',
        element: (
          <Placeholder
            title="执行与验证"
            source="任务书 · 智能割接维护（操作 / 验证）"
            status="planned"
            summary="割接自动化率考核依赖执行编排与拨测验证，当前无执行侧能力。"
            items={[
              '割接操作编排与自动执行',
              '业务拨测点验证清单自动生成',
              '业务指标（吞吐/时延）与资源指标（CPU/内存）比对基线',
              '执行结果智能判定与异常自动回退',
            ]}
          />
        ),
      },
    ],
  },
  {
    key: 'governance',
    label: '云网数据治理',
    icon: <DatabaseOutlined />,
    items: [
      {
        key: '/op-topology',
        label: '运维关系拓扑',
        status: 'done',
        element: <OpTopology />,
      },
      {
        key: '/audit',
        label: '资源稽核',
        status: 'done',
        element: <Audit />,
      },
      {
        key: '/relations',
        label: '关系修正',
        status: 'done',
        element: <Relations />,
      },
      {
        key: '/topology',
        label: '资源拓扑与影响面',
        status: 'partial',
        element: <Topology />,
      },
      {
        key: '/topo-sync',
        label: '拓扑数据同步',
        status: 'partial',
        element: <TopoSync />,
      },
      {
        key: '/resources',
        label: '运维对象',
        status: 'partial',
        element: (
          <Placeholder
            title="运维对象"
            source="任务书 · 功能清单（运维对象）"
            status="partial"
            summary="当前仅覆盖业务应用与中间件（含 MySQL/TiDB/Redis 等），其余对象类型尚未建模。"
            foundation={['业务应用', '中间件 / 数据库', '依赖关系与实例地址']}
            items={[
              'K8S、容器、系统组件',
              '主机 / 操作系统 / 存储 / 网络 / 机房',
              '对象属性、责任人、环境标签维护',
              '对象与 CMDB 编码映射',
            ]}
            related={{ label: '去看运维关系拓扑', to: '/op-topology' }}
          />
        ),
      },
    ],
  },
  {
    key: 'quality',
    label: '网络及服务质量优化',
    icon: <LineChartOutlined />,
    items: [
      {
        key: '/quality',
        label: '质差分析',
        status: 'done',
        element: <Quality />,
      },
      {
        key: '/degradation',
        label: '指标劣化',
        status: 'done',
        element: <Degradation />,
      },
    ],
  },
  {
    key: 'outcome',
    label: '运营指标',
    icon: <TrophyOutlined />,
    toggle: 'outcome',
    items: [
      {
        key: '/metrics',
        label: '考核指标看板',
        status: 'done',
        element: <Metrics />,
      },
    ],
  },
  {
    key: 'capability',
    label: '通用能力',
    icon: <RobotOutlined />,
    items: [
      { key: '/mcp', label: '数据与能力接口', status: 'partial', element: <McpConsole /> },
      {
        key: '/knowledge',
        label: '历史故障库与知识库',
        status: 'done',
        element: <Knowledge />,
      },
      {
        key: '/platform',
        label: '平台服务',
        status: 'partial',
        element: (
          <Placeholder
            title="平台服务"
            source="任务书 · 成果形态（平台服务 / API 能力开放）"
            status="partial"
            summary="已提供 REST + SSE 能力接口，但缺鉴权、租户、OpenAPI 文档与正式交付物。"
            foundation={['REST API + SSE 事件流', '统一接入网关与调用审计', '文件 + PG 备份存储']}
            items={[
              '接口鉴权、租户与权限模型',
              'OpenAPI / 能力目录与调用配额',
              '使用手册、部署文档、测试报告',
              '运行指标（发现/定位/恢复时长）埋点与看板',
            ]}
            related={{ label: '去数据源控制台', to: '/mcp' }}
          />
        ),
      },
    ],
  },
  {
    key: 'ai',
    label: '模型与算法',
    icon: <DeploymentUnitOutlined />,
    toggle: 'ai',
    plannedGroup: true,
    items: [
      {
        key: '/samples',
        label: '样本库管理',
        status: 'planned',
        element: (
          <Placeholder
            title="样本库管理"
            source="任务书 · 通用能力（样本库管理）"
            status="planned"
            summary="目前规则与阈值均为人工配置，尚无标注样本与数据集管理。"
            items={['标注样本集管理', '样本导入 / 导出', '样本质量校验', '按运维场景划分数据集']}
          />
        ),
      },
      {
        key: '/features',
        label: '特征库管理',
        status: 'planned',
        element: (
          <Placeholder
            title="特征库管理"
            source="任务书 · 通用能力（特征库 / 特征提取）"
            status="planned"
            summary="特征提取器与特征集尚未建设，排障仍依赖规则打分。"
            items={['特征提取器管理', '特征集版本化', '特征与标签关联', '特征质量评估']}
          />
        ),
      },
      {
        key: '/preprocess',
        label: '数据预处理与预检验',
        status: 'planned',
        element: (
          <Placeholder
            title="数据预处理与预检验"
            source="任务书 · 通用能力（数据预处理 / 数据预检验）"
            status="planned"
            summary="时序数据清洗与缺失值填充尚未建设，指标数据直接透传。"
            items={['时序缺失值填充', '异常数据注入与清洗', '数据预检验规则', '数据质量报告']}
          />
        ),
      },
      {
        key: '/algorithms',
        label: '算法管理',
        status: 'planned',
        element: (
          <Placeholder
            title="算法管理"
            source="任务书 · 通用能力（算法管理）"
            status="planned"
            summary="当前异常检测与根因定位均为规则实现，无算法注册与版本管理。"
            items={['算法注册与参数配置', '算法版本管理', '算法与场景绑定', '算法效果对比']}
          />
        ),
      },
      {
        key: '/training',
        label: '模型训练',
        status: 'planned',
        element: (
          <Placeholder
            title="模型训练"
            source="任务书 · 通用能力（模型训练）"
            status="planned"
            summary="无训练任务与评估链路，模型能力尚未接入排障/巡检。"
            items={['训练任务编排', '训练指标与评估', '模型发布', '训练资源与日志']}
          />
        ),
      },
      {
        key: '/models',
        label: '模型库管理',
        status: 'planned',
        element: (
          <Placeholder
            title="模型库管理"
            source="任务书 · 通用能力（模型库管理）"
            status="planned"
            summary="尚无模型版本、灰度与推理配置管理。"
            items={['模型版本与灰度', '推理服务配置', '模型效果监控', '模型下线与回滚']}
          />
        ),
      },
    ],
  },
];

export const ALL_ITEMS = NAV.flatMap((g) => g.items);
