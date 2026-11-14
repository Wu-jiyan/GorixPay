import http from './client';

export interface CleanupResultRow {
  target?: string;
  matched?: number;
  deleted?: number;
}

export interface CleanupResponse {
  results?: CleanupResultRow[];
}

export interface CleanupInput {
  targets: string[];
  before: string;
  dry_run: boolean;
  confirm?: string;
}

export const maintenanceApi = {
  async cleanup(payload: CleanupInput) {
    const { data } = await http.post<CleanupResponse>('/admin/maintenance/cleanup', payload);
    return data;
  },
};
