import http from './client';

export interface VoiceDevice {
  id: string;
  device_no: string;
  name?: string;
  status?: string;
  last_seen_at?: string | null;
  created_at?: string;
  updated_at?: string;
  /** 只在创建响应里出现一次 */
  secret?: string;
}

export interface VoiceDeviceInput {
  device_no: string;
  name: string;
  status?: string;
}

export interface Printer {
  id: string;
  name: string;
  status?: string;
  created_at?: string;
  updated_at?: string;
  /** 只在创建响应里出现一次 */
  secret?: string;
}

export interface PrinterInput {
  name: string;
  status?: string;
}

export const devicesApi = {
  async voiceDevices() {
    const { data } = await http.get<{ items: VoiceDevice[] } | VoiceDevice[]>('/admin/voice-devices');
    return data;
  },
  async createVoiceDevice(payload: VoiceDeviceInput) {
    const { data } = await http.post<VoiceDevice>('/admin/voice-devices', payload);
    return data;
  },
  async updateVoiceDevice(id: string, payload: VoiceDeviceInput) {
    const { data } = await http.put<VoiceDevice>(`/admin/voice-devices/${id}`, payload);
    return data;
  },
  async deleteVoiceDevice(id: string) {
    await http.delete(`/admin/voice-devices/${id}`);
  },
  async testVoiceDevice(id: string, text: string) {
    const { data } = await http.post<{ ok?: boolean; message?: string }>(`/admin/voice-devices/${id}/test`, { text });
    return data;
  },

  async printers() {
    const { data } = await http.get<{ items: Printer[] } | Printer[]>('/admin/printers');
    return data;
  },
  async createPrinter(payload: PrinterInput) {
    const { data } = await http.post<Printer>('/admin/printers', payload);
    return data;
  },
  async updatePrinter(id: string, payload: PrinterInput) {
    const { data } = await http.put<Printer>(`/admin/printers/${id}`, payload);
    return data;
  },
  async deletePrinter(id: string) {
    await http.delete(`/admin/printers/${id}`);
  },
  async testPrinter(id: string) {
    const { data } = await http.post<{ ok?: boolean; message?: string }>(`/admin/printers/${id}/test`);
    return data;
  },
};
