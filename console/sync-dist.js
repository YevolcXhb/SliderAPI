// 把 new-api/web/dist 同步到 console/public
// 保留 deepseek-theme.css，并给 index.html 注入标题与主题引用
// 用 node 写文件，避免 PowerShell 的 UTF-8 编码坑
const fs = require('fs');
const path = require('path');

const src = 'C:/Users/32927/Desktop/SliderAPI/new-api/web/dist';
const pub = 'C:/Users/32927/Desktop/SliderAPI/console/public';

// 1) 清理旧产物，保留 deepseek-theme.css
for (const name of fs.readdirSync(pub)) {
  if (name === 'deepseek-theme.css') continue;
  fs.rmSync(path.join(pub, name), { recursive: true, force: true });
}
console.log('cleaned old artifacts (kept deepseek-theme.css)');

// 2) 递归拷贝
fs.cpSync(src, pub, { recursive: true });
console.log('copied new build output');

// 3) 注入标题与主题引用
const idx = path.join(pub, 'index.html');
let html = fs.readFileSync(idx, 'utf8');
html = html.replace(/<title>[^<]*<\/title>/, '<title>SliderAPI</title>');
html = html.replace(/<meta name="title" content="[^"]*"\s*\/>/, '<meta name="title" content="SliderAPI" />');
if (!html.includes('deepseek-theme')) {
  html = html.replace('</head>', '<link href="/deepseek-theme.css" rel="stylesheet"></head>');
}
fs.writeFileSync(idx, html, 'utf8');
console.log('injected title and theme link');

// 校验
const check = fs.readFileSync(idx, 'utf8');
console.log('');
console.log('=== verify ===');
console.log('index has SliderAPI:', check.includes('SliderAPI'));
console.log('index has deepseek-theme:', check.includes('deepseek-theme'));
console.log('deepseek-theme.css exists:', fs.existsSync(path.join(pub, 'deepseek-theme.css')));
let count = 0;
(function walk(d) {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    if (e.isDirectory()) walk(path.join(d, e.name));
    else count++;
  }
})(pub);
console.log('total files:', count);
