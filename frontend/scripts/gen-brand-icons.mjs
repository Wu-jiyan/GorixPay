// 从 simple-icons 抽出站点要用的品牌图标，生成 src/components/brandIcons.ts。
// 之所以生成静态文件而不是运行时 import：simple-icons 整个包有几 MB，
// 只要用到五六个图标，按需提取路径字符串能少几百 KB。
// 用法：node scripts/gen-brand-icons.mjs
import { writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as si from 'simple-icons';

const here = dirname(fileURLToPath(import.meta.url));
const target = resolve(here, '../src/components/brandIcons.ts');

const wanted = {
  alipay: 'siAlipay',
  wechat: 'siWechat',
  qq: 'siQq',
  visa: 'siVisa',
  mastercard: 'siMastercard',
  applepay: 'siApplepay',
  googlepay: 'siGooglepay',
  paypal: 'siPaypal',
  stripe: 'siStripe',
  bitcoin: 'siBitcoin',
  tether: 'siTether',
};

const out = {};
const missing = [];
for (const [key, exportName] of Object.entries(wanted)) {
  const icon = si[exportName];
  if (!icon) {
    missing.push(exportName);
    continue;
  }
  out[key] = { title: icon.title, hex: icon.hex, path: icon.path };
}
if (missing.length > 0) {
  console.warn('simple-icons 里找不到：', missing.join(', '));
}

const source = `// 由 scripts/gen-brand-icons.mjs 从 simple-icons 生成（CC0 1.0），请勿手改。
// 只提取站点用到的品牌，避免把整包几 MB 的图标打进产物。

export interface BrandIcon {
  title: string;
  hex: string;
  path: string;
}

export const brandIcons = ${JSON.stringify(out, null, 2)} as const satisfies Record<string, BrandIcon>;

export type BrandIconKey = keyof typeof brandIcons;
`;

writeFileSync(target, source, 'utf8');
console.log(`wrote ${target} (${source.length} bytes, ${Object.keys(out).length} icons)`);
