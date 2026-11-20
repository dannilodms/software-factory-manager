import { readdir, readFile, writeFile } from "node:fs/promises"
import { join, resolve } from "node:path"
import { promisify } from "node:util"
import { brotliCompress, constants } from "node:zlib"

import type { Plugin } from "vite-plus"

const compress = promisify(brotliCompress)

// The text files of the build, which get a Brotli copy: JavaScript, CSS,
// HTML, SVG and JSON. A font is compressed already.
const textFile = /\.(js|css|html|svg|json)$/

// Writes a Brotli copy (`.br`) beside each text file of the build, once the
// build has written every file, at the highest quality: it is done once, and
// Caddy serves the copy to a browser that accepts Brotli (Compression in
// Delivery, CODING_STANDARDS.md). The build empties its folder first, so no
// copy outlives its file.
export function brotliCopies(): Plugin {
  let outDir = ""
  return {
    name: "brotli-copies",
    apply: "build",
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },
    async closeBundle() {
      const files = await readdir(outDir, { recursive: true })
      await Promise.all(
        files
          .filter((file) => textFile.test(file))
          .map((file) => writeCopy(join(outDir, file)))
      )
    },
  }
}

async function writeCopy(path: string) {
  const content = await readFile(path)
  const copy = await compress(content, {
    params: {
      [constants.BROTLI_PARAM_MODE]: constants.BROTLI_MODE_TEXT,
      [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY,
      [constants.BROTLI_PARAM_SIZE_HINT]: content.length,
    },
  })
  await writeFile(`${path}.br`, copy)
}
