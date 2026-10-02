import type { EngineInterface, Register } from 'claude-code'

// Written beside this mod by the warden on every spawn (cli/ocwarden/notifymod.go
// notifyModConfigFile); every path and name below comes from it.
const CONFIG_FILE = 'officraft.json'

type Config = {
  loaded_marker: string
  disabled_marker: string
  ack_file: string
  listener: { argv: string[]; cwd: string; env: Record<string, string> }
}

type Frame = { submit: string } | { batch: string }

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    // Without a config the mod stays silent: the warden then finds no load marker
    // and falls back to its tmux paste listener.
    const config = await readConfig($)
    if (config === undefined) return started
    // 🔴 The warden fell back to its tmux paste listener for this session; a
    // second listener on the same identity makes the station evict one of them.
    if (await $.fs.exists(config.disabled_marker)) return started
    await $.fs.write(config.loaded_marker, 'loaded\n')
    void listen($, config)
    return started
  })
}

async function readConfig($: EngineInterface): Promise<Config | undefined> {
  const path = `${$.plugin.root}/${CONFIG_FILE}`
  try {
    const value: unknown = JSON.parse(await $.fs.read(path))
    if (isConfig(value)) return value
    $.ui.log(`${path} is not a notification config; not listening`, { to: 'debug' })
  } catch (err) {
    $.ui.log(`cannot read ${path} (${String(err)}); not listening`, { to: 'debug' })
  }
  return undefined
}

function isConfig(value: unknown): value is Config {
  if (typeof value !== 'object' || value === null) return false
  const c = value as Record<string, unknown>
  const listener = c.listener as Record<string, unknown> | undefined
  return (
    typeof c.loaded_marker === 'string' &&
    typeof c.disabled_marker === 'string' &&
    typeof c.ack_file === 'string' &&
    typeof listener === 'object' &&
    listener !== null &&
    Array.isArray(listener.argv) &&
    listener.argv.every(a => typeof a === 'string') &&
    typeof listener.cwd === 'string' &&
    typeof listener.env === 'object' &&
    listener.env !== null &&
    Object.values(listener.env).every(v => typeof v === 'string')
  )
}

// The child lives as long as this loop: leaving it, or the module unloading,
// kills it. A listener that exits is not restarted: the connection it held
// disappearing is what makes the station recycle this member.
async function listen($: EngineInterface, config: Config): Promise<void> {
  const child = $.process.spawn({
    argv: config.listener.argv,
    cwd: config.listener.cwd,
    env: config.listener.env,
  })
  let pending = ''
  let batchDelivered = true
  try {
    for await (const { stream, text } of child) {
      if (stream === 'stderr') {
        $.ui.log(text, { to: 'debug' })
        continue
      }
      pending += text
      for (let nl = pending.indexOf('\n'); nl >= 0; nl = pending.indexOf('\n')) {
        const line = pending.slice(0, nl)
        pending = pending.slice(nl + 1)
        const frame = parseFrame(line)
        if (frame === undefined) {
          if (line.trim() !== '') $.ui.log(line, { to: 'debug' })
        } else if ('submit' in frame) {
          batchDelivered = (await submitted($, frame.submit)) && batchDelivered
        } else {
          // Answered only after every submit before the marker settled: the
          // listener marks the batch read on the station on `ack`.
          await $.fs.write(config.ack_file, `${batchDelivered ? 'ack' : 'nack'} ${frame.batch}\n`)
          batchDelivered = true
        }
      }
    }
    const { code, signal } = await child.result
    $.ui.log(`ocagent listen exited (code ${code}, signal ${signal})`, { to: 'debug' })
  } catch (err) {
    $.ui.log(`ocagent listen stopped: ${String(err)}`, { to: 'debug' })
  }
}

function parseFrame(line: string): Frame | undefined {
  try {
    const value: unknown = JSON.parse(line)
    if (typeof value !== 'object' || value === null) return undefined
    const fields = value as Record<string, unknown>
    if (typeof fields.submit === 'string') return { submit: fields.submit }
    if (typeof fields.batch === 'string') return { batch: fields.batch }
  } catch {
    return undefined
  }
  return undefined
}

async function submitted($: EngineInterface, text: string): Promise<boolean> {
  try {
    const result = await $.prompt.submit({ text })
    return result.drop === undefined
  } catch {
    return false
  }
}
