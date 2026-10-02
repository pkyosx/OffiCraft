import type { EngineInterface, Register } from 'claude-code'

// The warden (cli/ocwarden/spawn.go) reads the first, writes the second, and
// spells all three file names again.
const LOADED_MARKER = '.officraft-mod-loaded'
const DISABLED_MARKER = '.officraft-mod-disabled'
const ACK_FILE = '.officraft-listen-ack'

type Frame = { submit: string } | { batch: string }

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    // 🔴 The warden fell back to its tmux paste listener for this session; a
    // second listener on the same identity makes the station evict one of them.
    if (await $.fs.exists(`${e.cwd}/${DISABLED_MARKER}`)) return started
    await $.fs.write(`${e.cwd}/${LOADED_MARKER}`, 'loaded\n')
    void listen($, e.cwd)
    return started
  })
}

// The child lives as long as this loop: leaving it, or the module unloading,
// kills it. A listener that exits is not restarted: the connection it held
// disappearing is what makes the station recycle this member.
async function listen($: EngineInterface, cwd: string): Promise<void> {
  const ackFile = `${cwd}/${ACK_FILE}`
  const child = $.process.spawn({
    argv: [`${cwd}/ocagent`, 'listen', '--deliver-mod'],
    cwd,
    env: { OC_LISTEN_ACK: '1', OC_LISTEN_ACK_FILE: ackFile },
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
          await $.fs.write(ackFile, `${batchDelivered ? 'ack' : 'nack'} ${frame.batch}\n`)
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
