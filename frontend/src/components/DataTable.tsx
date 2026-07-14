import React from 'react';
import { Table, TableProps } from '@arco-design/web-react';
import { PaginatedResponse } from '../api/admin';

interface Props<T> extends Omit<TableProps<T>, 'data' | 'pagination'> {
  data: PaginatedResponse<T> | undefined;
  loading?: boolean;
}

function DataTable<T extends object>({ data, loading, ...rest }: Props<T>) {
  return (
    <Table
      data={data?.items || []}
      loading={loading}
      pagination={
        data
          ? { total: data.total, current: data.page, pageSize: data.page_size, sizeCanChange: true }
          : false
      }
      {...rest}
    />
  );
}

export default DataTable;
