import React from 'react';
import { Tag } from '@arco-design/web-react';

const colorMap: Record<string, string> = {
  active: 'green',
  disabled: 'red',
  pending: 'orange',
  processing: 'blue',
  paid: 'green',
  closed: 'gray',
  expired: 'gray',
  completed: 'green',
  rejected: 'red',
  cancelled: 'gray',
};

interface Props {
  status: string;
}

const StatusTag: React.FC<Props> = ({ status }) => {
  return <Tag color={colorMap[status] || 'gray'}>{status}</Tag>;
};

export default StatusTag;
