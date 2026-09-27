// npm 配布用パッケージを dist/npm/ に生成し、必要なら公開する。
//
//   node npm/build.mjs <version>             ビルドのみ（例: 0.1.0）
//   node npm/build.mjs <version> --publish   ビルドして npm に公開（公開済みの版は飛ばす）
//   node npm/build.mjs <version> --pack      ビルドして .tgz を作る（ローカル確認用）
//
// 本体パッケージ（npm/package/）に JS シムを置き、OS/CPU 別のバイナリは
// <本体名>-<os>-<cpu> パッケージとして optionalDependencies に並べる（esbuild と同じ方式）。
import { execFileSync } from 'node:child_process';
import { chmodSync, cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const BIN = 'bin'; // コマンド名
const MAIN = './cmd/bin'; // go build の対象
const LDFLAGS = '-X main.channel=npm'; // 追加の -X（npm 版であることを埋め込むなど）

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const out = join(root, 'dist', 'npm');

// [node の platform, node の arch, GOOS, GOARCH]
const targets = [
  ['linux', 'x64', 'linux', 'amd64'],
  ['linux', 'arm64', 'linux', 'arm64'],
  ['darwin', 'x64', 'darwin', 'amd64'],
  ['darwin', 'arm64', 'darwin', 'arm64'],
  ['win32', 'x64', 'windows', 'amd64'],
];

const [version, ...flags] = process.argv.slice(2);
if (!/^\d+\.\d+\.\d+(-[\w.]+)?$/.test(version ?? '')) {
  console.error('使い方: node npm/build.mjs <version> [--publish|--pack]');
  process.exit(2);
}

const main = JSON.parse(readFileSync(join(root, 'npm', 'package', 'package.json'), 'utf8'));
const run = (cmd, args, opts = {}) => execFileSync(cmd, args, { stdio: 'inherit', ...opts });

// 途中で失敗しても再実行で続きから公開できるように、公開済みの版は飛ばす
const published = (name, v) => {
  try {
    return execFileSync('npm', ['view', `${name}@${v}`, 'version'], { stdio: ['ignore', 'pipe', 'ignore'] })
      .toString().trim() === v;
  } catch {
    return false;
  }
};

rmSync(out, { recursive: true, force: true });
const dirs = [];
main.optionalDependencies = {};

for (const [os, cpu, goos, goarch] of targets) {
  const name = `${main.name}-${os}-${cpu}`;
  const dir = join(out, `${os}-${cpu}`);
  const exe = BIN + (os === 'win32' ? '.exe' : '');
  mkdirSync(join(dir, 'bin'), { recursive: true });
  console.log(`==> ${name}`);
  run('go', ['build', '-trimpath', '-buildvcs=false',
    '-ldflags', `-s -w -X main.version=${version} ${LDFLAGS}`.trim(),
    '-o', join(dir, 'bin', exe), MAIN], {
    cwd: root,
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
  });
  writeFileSync(join(dir, 'package.json'), JSON.stringify({
    name,
    version,
    description: `${main.name} の ${os}-${cpu} 用バイナリ`,
    repository: main.repository,
    license: main.license,
    os: [os],
    cpu: [cpu],
    files: [`bin/${exe}`],
  }, null, 2) + '\n');
  main.optionalDependencies[name] = version;
  dirs.push(dir);
}

const mainDir = join(out, 'main');
cpSync(join(root, 'npm', 'package'), mainDir, { recursive: true });
cpSync(join(root, 'README.md'), join(mainDir, 'README.md'));
cpSync(join(root, 'LICENSE'), join(mainDir, 'LICENSE'));
chmodSync(join(mainDir, 'bin', BIN), 0o755);
writeFileSync(join(mainDir, 'package.json'), JSON.stringify({ ...main, version }, null, 2) + '\n');
dirs.push(mainDir); // 本体はバイナリより後に公開する

if (flags.includes('--publish')) {
  const extra = process.env.GITHUB_ACTIONS ? ['--provenance'] : [];
  for (const dir of dirs) {
    const { name } = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'));
    if (published(name, version)) {
      console.log(`==> 公開済みなので飛ばす: ${name}@${version}`);
      continue;
    }
    run('npm', ['publish', '--access', 'public', ...extra], { cwd: dir });
  }
} else if (flags.includes('--pack')) {
  for (const dir of dirs) run('npm', ['pack', '--pack-destination', out], { cwd: dir });
}
console.log(`==> 完了: ${out}`);
