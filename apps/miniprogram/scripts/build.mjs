import { readdir, readFile, writeFile } from 'node:fs/promises';
import { watch } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

const appRoot = fileURLToPath(new URL('../app/', import.meta.url));
const excluded = new Set(['miniprogram_npm', 'node_modules']);

async function sources(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    if (excluded.has(entry.name)) continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await sources(path));
    else if (entry.name.endsWith('.ts') && !entry.name.endsWith('.d.ts')) files.push(path);
  }
  return files;
}

async function saveChanged(path, text) {
  const previous = await readFile(path, 'utf8').catch(() => null);
  if (previous !== text) await writeFile(path, text, 'utf8');
}

async function build() {
  const files = await sources(appRoot);
  for (const source of files) {
    const result = ts.transpileModule(await readFile(source, 'utf8'), {
      fileName: source,
      reportDiagnostics: true,
      compilerOptions: {
        target: ts.ScriptTarget.ES2018,
        module: ts.ModuleKind.CommonJS,
        sourceMap: true,
        strict: true,
      },
    });
    const errors = (result.diagnostics || []).filter((item) => item.category === ts.DiagnosticCategory.Error);
    if (errors.length) {
      throw new Error(ts.formatDiagnosticsWithColorAndContext(errors, {
        getCurrentDirectory: () => dirname(appRoot),
        getCanonicalFileName: (path) => path,
        getNewLine: () => '\n',
      }));
    }
    await saveChanged(source.replace(/\.ts$/, '.js'), result.outputText);
    if (result.sourceMapText) await saveChanged(source.replace(/\.ts$/, '.js.map'), result.sourceMapText);
  }
  console.log(`Compiled ${files.length} mini program TypeScript files.`);
}

await build();

if (process.argv.includes('--watch')) {
  let timer;
  let running = Promise.resolve();
  watch(appRoot, { recursive: true }, (_event, file) => {
    if (!file || !file.endsWith('.ts') || file.endsWith('.d.ts')) return;
    if (file.split(/[\\/]/).some((part) => excluded.has(part))) return;
    clearTimeout(timer);
    timer = setTimeout(() => {
      running = running.then(build).catch((error) => console.error(error.message));
    }, 120);
  });
  console.log('Watching mini program TypeScript files. Run typecheck separately while editing.');
}
