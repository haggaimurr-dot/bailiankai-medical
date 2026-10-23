import { copyFile, mkdir } from 'node:fs/promises';
const root = new URL('../', import.meta.url);
for (const name of ['mlk.web.css', 'mlk.mobile.wxss']) {
  await mkdir(new URL('packages/design-tokens/', root), { recursive: true });
  await copyFile(new URL(`美莲凯_UI设计规范_Figma实施资源包/code/${name}`, root), new URL(`packages/design-tokens/${name}`, root));
}
await mkdir(new URL('apps/miniprogram/app/styles/', root), { recursive: true });
await copyFile(new URL('packages/design-tokens/mlk.mobile.wxss', root), new URL('apps/miniprogram/app/styles/mlk.mobile.wxss', root));
console.log('Synced web and mini program design tokens from the design resource package.');
