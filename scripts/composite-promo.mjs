import sharp from 'sharp'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const root = path.join(__dirname, '..')
const shots = path.join(root, 'docs', 'screenshots')
const out = path.join(shots, 'promo.png')

const W = 300
const H = 580
const pad = 56
const gap = 28
const titleH = 96
const radius = 14

async function roundCorners(file) {
  const img = sharp(path.join(shots, file))
  const mask = Buffer.from(
    `<svg width="${W}" height="${H}"><rect width="${W}" height="${H}" rx="${radius}" ry="${radius}"/></svg>`,
  )
  return img.ensureAlpha().composite([{ input: mask, blend: 'dest-in' }]).png().toBuffer()
}

const dockMeta = await sharp(path.join(shots, 'dock.png')).metadata()
const dockW = Math.round(dockMeta.width * (H / dockMeta.height))
const dockBuf = await sharp(path.join(shots, 'dock.png'))
  .resize({ height: H })
  .ensureAlpha()
  .composite([
    {
      input: Buffer.from(
        `<svg width="${dockW}" height="${H}"><rect width="${dockW}" height="${H}" rx="${radius}" ry="${radius}"/></svg>`,
      ),
      blend: 'dest-in',
    },
  ])
  .png()
  .toBuffer()

const [home, nodes, settings] = await Promise.all([
  roundCorners('home.png'),
  roundCorners('nodes.png'),
  roundCorners('settings.png'),
])

const canvasW = pad * 2 + W * 3 + gap * 2 + gap + dockW
const canvasH = pad * 2 + titleH + H + 36

const labels = [
  { text: '连接', x: pad + W / 2 },
  { text: '节点', x: pad + W + gap + W / 2 },
  { text: '设置', x: pad + (W + gap) * 2 + W / 2 },
  { text: '侧边栏', x: pad + (W + gap) * 3 + dockW / 2 },
]

const labelSvg = labels
  .map(
    (l) =>
      `<text x="${l.x}" y="${pad + titleH + H + 24}" text-anchor="middle" font-family="Segoe UI, PingFang SC, Microsoft YaHei, sans-serif" font-size="13" fill="#64748b">${l.text}</text>`,
  )
  .join('')

const headerSvg = Buffer.from(
  `<svg width="${canvasW}" height="${canvasH}" xmlns="http://www.w3.org/2000/svg">
    <text x="${canvasW / 2}" y="58" text-anchor="middle" font-family="Segoe UI, PingFang SC, Microsoft YaHei, sans-serif" font-size="34" font-weight="600" fill="#0f172a">EasyClash</text>
    <text x="${canvasW / 2}" y="88" text-anchor="middle" font-family="Segoe UI, PingFang SC, Microsoft YaHei, sans-serif" font-size="15" fill="#64748b">极简桌面代理 · 一键连接 · 智能测速</text>
    ${labelSvg}
  </svg>`,
)

const bgSvg = Buffer.from(
  `<svg width="${canvasW}" height="${canvasH}" xmlns="http://www.w3.org/2000/svg">
    <defs>
      <linearGradient id="bg" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#f8fafc"/>
        <stop offset="55%" stop-color="#f1f5f9"/>
        <stop offset="100%" stop-color="#e2e8f0"/>
      </linearGradient>
    </defs>
    <rect width="100%" height="100%" fill="url(#bg)"/>
  </svg>`,
)

const y = pad + titleH
const composites = [
  { input: bgSvg, top: 0, left: 0 },
  { input: home, top: y, left: pad },
  { input: nodes, top: y, left: pad + W + gap },
  { input: settings, top: y, left: pad + (W + gap) * 2 },
  { input: dockBuf, top: y, left: pad + (W + gap) * 3 },
  { input: headerSvg, top: 0, left: 0 },
]

await sharp({
  create: {
    width: canvasW,
    height: canvasH,
    channels: 4,
    background: { r: 248, g: 250, b: 252, alpha: 1 },
  },
})
  .composite(composites)
  .png({ compressionLevel: 9 })
  .toFile(out)

const meta = await sharp(out).metadata()
console.log(`promo: ${out} (${meta.width}x${meta.height})`)
