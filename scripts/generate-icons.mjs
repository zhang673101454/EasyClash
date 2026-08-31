import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import sharp from 'sharp'
import pngToIco from 'png-to-ico'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(__dirname, '..')
const svgPath = path.join(root, 'build', 'icon.svg')
const svgBuffer = await fs.readFile(svgPath)

async function renderPng(size) {
  return sharp(svgBuffer, { density: Math.max(144, Math.round(size * 0.18)) })
    .resize(size, size)
    .png()
    .toBuffer()
}

const sizes = [16, 32, 48, 64, 128, 256]
const pngSizes = await Promise.all(sizes.map((size) => renderPng(size)))
const appIcon = await renderPng(1024)
const logo512 = await renderPng(512)
const icoBuffer = await pngToIco(pngSizes)

await fs.mkdir(path.join(root, 'build', 'windows'), { recursive: true })
await fs.mkdir(path.join(root, 'frontend', 'src', 'assets', 'images'), { recursive: true })

await fs.writeFile(path.join(root, 'build', 'appicon.png'), appIcon)
await fs.writeFile(path.join(root, 'build', 'windows', 'icon.ico'), icoBuffer)
await fs.writeFile(path.join(root, 'tray.ico'), icoBuffer)
await fs.writeFile(path.join(root, 'tray.png'), pngSizes[1])
await fs.writeFile(path.join(root, 'frontend', 'src', 'assets', 'images', 'logo-universal.png'), logo512)

console.log('Icons generated successfully.')
