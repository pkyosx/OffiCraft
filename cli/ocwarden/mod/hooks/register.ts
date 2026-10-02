import type { EngineInterface, Register } from 'claude-code'

// Written beside this mod by the warden on every spawn (cli/ocwarden/notifymod.go
// notifyModConfigFile); every path and name below comes from it.
const CONFIG_FILE = 'officraft.json'

// A plugin submit resolves only when its turn STARTS, minutes later while the
// member is busy, far past the listener's 30 s ack wait; a refusal comes at once.
const ACCEPT_GRACE_MS = 1500
// ⚠️ While the member is busy each submit spends the whole grace, one after the
// other: a batch of more than ~20 payloads (30 s ack wait / 1.5 s) outlasts the
// listener's ack wait and brings the reprint loop back.

type Config = {
  boot_prompt: string
  loaded_marker: string
  disabled_marker: string
  booted_marker: string
  ack_file: string
  ready_prefixes: string[]
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
    void boot($, config)
    return started
  })
}

// Detached from session.start, which the engine awaits before the first prompt:
// the boot prompt is waited on here instead.
async function boot($: EngineInterface, config: Config): Promise<void> {
  // 🔴 The boot prompt goes in BEFORE the listener exists: a backlog the listener
  // prints on connect queues behind it instead of becoming the member's first
  // turn (and being marked read before the member ever booted).
  if (!(await accepted($, config.boot_prompt, true))) {
    $.ui.log('the boot prompt was refused; not listening', { to: 'debug' })
    return
  }
  // Tells a warden that falls back to pasting not to paste a second boot prompt.
  await $.fs.write(config.booted_marker, 'booted\n')
  await listen($, config)
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
    typeof c.boot_prompt === 'string' &&
    typeof c.loaded_marker === 'string' &&
    typeof c.disabled_marker === 'string' &&
    typeof c.booted_marker === 'string' &&
    typeof c.ack_file === 'string' &&
    Array.isArray(c.ready_prefixes) &&
    c.ready_prefixes.every(p => typeof p === 'string') &&
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
//
// The load marker waits for the listener's first frame or transport line: one
// that refused to start prints neither, and the missing marker is what sends
// the warden to its paste fallback.
async function listen($: EngineInterface, config: Config): Promise<void> {
  const child = $.process.spawn({
    argv: config.listener.argv,
    cwd: config.listener.cwd,
    env: config.listener.env,
  })
  let pending = ''
  let pendingErr = ''
  let ready = false
  let batchDelivered = true
  // false: the warden fell back meanwhile, so this listener must go.
  const markReady = async (): Promise<boolean> => {
    if (ready) return true
    if (await $.fs.exists(config.disabled_marker)) {
      $.ui.log('the warden fell back to pasting; stopping this listener', { to: 'debug' })
      return false
    }
    await $.fs.write(config.loaded_marker, 'loaded\n')
    ready = true
    return true
  }
  try {
    for await (const { stream, text } of child) {
      if (stream === 'stderr') {
        $.ui.log(text, { to: 'debug' })
        pendingErr += text
        let isTransport = false
        for (let nl = pendingErr.indexOf('\n'); nl >= 0; nl = pendingErr.indexOf('\n')) {
          const line = pendingErr.slice(0, nl)
          pendingErr = pendingErr.slice(nl + 1)
          if (config.ready_prefixes.some(prefix => line.startsWith(prefix))) isTransport = true
        }
        if (isTransport && !(await markReady())) return
        continue
      }
      pending += text
      for (let nl = pending.indexOf('\n'); nl >= 0; nl = pending.indexOf('\n')) {
        const line = pending.slice(0, nl)
        pending = pending.slice(nl + 1)
        const frame = parseFrame(line)
        if (frame !== undefined && !(await markReady())) return
        if (frame === undefined) {
          if (line.trim() !== '') $.ui.log(line, { to: 'debug' })
        } else if ('submit' in frame) {
          batchDelivered = (await accepted($, frame.submit)) && batchDelivered
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

type SubmitOutcome = 'entered' | 'pending' | string

// True once the engine has the prompt queued: refused or thrown within the grace
// is a failure, anything still pending then counts as accepted. Each call returns
// before the next submit is issued, so prompts are queued in order.
async function accepted($: EngineInterface, text: string, asUser = false): Promise<boolean> {
  // The async wrapper turns a synchronous throw into a rejection, a failed submit.
  const outcome: Promise<SubmitOutcome> = (async () => $.prompt.submit(asUser ? { text, asUser: true } : { text }))().then(
    result => (result.drop === undefined ? 'entered' : `dropped (${result.drop})`),
    err => `threw (${String(err)})`,
  )
  const early = await Promise.race([outcome, $.clock.sleep(ACCEPT_GRACE_MS).then((): SubmitOutcome => 'pending')])
  if (early !== 'pending') return early === 'entered'
  // ⚠️ Acked as queued, so the listener has already marked it read: a refusal
  // arriving now loses the message, and this log line is the only trace.
  void outcome.then(late => {
    if (late !== 'entered') $.ui.log(`a queued prompt was ${late} after it was acked: ${text.slice(0, 120)}`, { to: 'debug' })
  })
  return true
}
