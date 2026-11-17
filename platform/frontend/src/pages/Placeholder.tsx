import { Alert, Card, Descriptions, Divider, List, Space, Typography } from 'antd';
import { CheckCircleTwoTone, ClockCircleOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import StatusPill from '../components/StatusPill';

export interface PlaceholderProps {
  title: string;
  source: string;
  status: 'partial' | 'planned';
  summary: string;
  items: string[];
  foundation?: string[];
  related?: { label: string; to: string };
}

const STATUS_TAG = {
  partial: { tone: 'warn', text: '部分实现' },
  planned: { tone: 'neutral', text: '规划中' },
} as const;

export default function Placeholder({
  title,
  source,
  status,
  summary,
  items,
  foundation,
  related,
}: PlaceholderProps) {
  const tag = STATUS_TAG[status];

  return (
    <Card
      title={
        <Space>
          <span>{title}</span>
          <StatusPill tone={tag.tone} dot>{tag.text}</StatusPill>
        </Space>
      }
      extra={related && <Link to={related.to}>{related.label} →</Link>}
      size="small"
    >
      <Alert type="info" showIcon message={summary} style={{ marginBottom: 16 }} />

      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="需求来源" span={2}>
          {source}
        </Descriptions.Item>
        <Descriptions.Item label="当前状态">
          <StatusPill tone={tag.tone} dot>{tag.text}</StatusPill>
        </Descriptions.Item>
        <Descriptions.Item label="实现方式">
          {status === 'planned' ? '菜单占位，能力待建设' : '已有基础能力，待补齐'}
        </Descriptions.Item>
        {!!foundation?.length && (
          <Descriptions.Item label="已有底座" span={2}>
            <Space wrap size={4}>
              {foundation.map((f) => (
                <StatusPill key={f} tone="ok">
                  <CheckCircleTwoTone twoToneColor="#52c41a" /> {f}
                </StatusPill>
              ))}
            </Space>
          </Descriptions.Item>
        )}
      </Descriptions>

      <Divider orientation="left" plain style={{ marginTop: 20 }}>
        <Space>
          <ClockCircleOutlined />
          <Typography.Text type="secondary">按任务书拆解的规划功能点</Typography.Text>
        </Space>
      </Divider>

      <List
        size="small"
        dataSource={items}
        renderItem={(t) => (
          <List.Item>
            <Space>
              <span className="plan-dot" />
              {t}
            </Space>
          </List.Item>
        )}
      />
    </Card>
  );
}
