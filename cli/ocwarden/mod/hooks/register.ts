import type { EngineInterface, Register } from 'claude-code'

// Written beside this mod by the warden on every spawn (cli/ocwarden/notifymod.go
// notifyModConfigFile); every path and name below comes from it.
const CONFIG_FILE = 'officraft.json'

type Config = {
  boot_prompt: string
  started_marker: string
  loaded_marker: string
  disabled_marker: string
  booted_marker: string
  ready_prefixes: string[]
  listener: { argv: string[]; cwd: string }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    // Without a config the mod stays silent: the warden then finds no load marker
    // and fails the start.
    const config = await readConfig($)
    // Diagnostics only, first thing and before every check: when the mod does not
    // load, the warden logs its presence and mtime, which tell a late
    // session.start from a mod that never ran.
    if (config !== undefined) await markStarted($, config)
    const started = await next(e)
    if (config === undefined) return started
    // 🔴 The warden gave up on this session and is tearing it down: a boot or a
    // listener now would mark messages read in a member about to be killed.
    if (await $.fs.exists(config.disabled_marker)) {
      $.ui.log('session.start found the warden already gave up on this session; not booting', { to: 'debug' })
      return started
    }
    void boot($, config)
    return started
  })
}

// Detached from session.start, which the engine awaits before the first prompt:
// the boot prompt is waited on here instead.
async function boot($: EngineInterface, config: Config): Promise<void> {
  // 🔴 The boot prompt goes in BEFORE the listener exists: a backlog the listener
  // delivers on connect queues behind it instead of becoming the member's first
  // turn (and being marked read before the member ever booted).
  if (!(await submitted($, config.boot_prompt))) {
    $.ui.log('the boot prompt was refused; not listening', { to: 'debug' })
    return
  }
  // Tells the warden this attempt booted the member, so it is not restarted.
  await $.fs.write(config.booted_marker, 'booted\n')
  await listen($, config)
}

async function submitted($: EngineInterface, text: string): Promise<boolean> {
  try {
    const result = await $.prompt.submit({ text, asUser: true })
    return result.drop === undefined
  } catch {
    return false
  }
}

// A failed write only loses the diagnosis; the session goes on.
async function markStarted($: EngineInterface, config: Config): Promise<void> {
  try {
    await $.fs.write(config.started_marker, `${new Date(await $.clock.now()).toISOString()}\n`)
  } catch (err) {
    $.ui.log(`cannot write ${config.started_marker} (${String(err)})`, { to: 'debug' })
  }
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
    typeof c.started_marker === 'string' &&
    typeof c.loaded_marker === 'string' &&
    typeof c.disabled_marker === 'string' &&
    typeof c.booted_marker === 'string' &&
    Array.isArray(c.ready_prefixes) &&
    c.ready_prefixes.every(p => typeof p === 'string') &&
    typeof listener === 'object' &&
    listener !== null &&
    Array.isArray(listener.argv) &&
    listener.argv.every(a => typeof a === 'string') &&
    typeof listener.cwd === 'string'
  )
}

// ⚠️ The listener finds this session's messaging socket only through
// CLAUDE_CODE_MESSAGING_SOCKET/TOKEN inherited from this Claude Code: a spawn
// that replaced the environment would leave the member deaf.
//
// The child lives as long as this loop: leaving it, or the module unloading,
// kills it. A listener that exits is not restarted: the connection it held
// disappearing is what makes the station recycle this member.
//
// The load marker waits for the listener's first transport line on stderr: one
// that refused to start prints none, and the missing marker is what fails the
// warden's start.
async function listen($: EngineInterface, config: Config): Promise<void> {
  const child = $.process.spawn({ argv: config.listener.argv, cwd: config.listener.cwd })
  let pendingErr = ''
  let ready = false
  try {
    for await (const { stream, text } of child) {
      $.ui.log(text, { to: 'debug' })
      if (stream !== 'stderr' || ready) continue
      pendingErr += text
      let isTransport = false
      for (let nl = pendingErr.indexOf('\n'); nl >= 0; nl = pendingErr.indexOf('\n')) {
        const line = pendingErr.slice(0, nl)
        pendingErr = pendingErr.slice(nl + 1)
        if (config.ready_prefixes.some(prefix => line.startsWith(prefix))) isTransport = true
      }
      if (!isTransport) continue
      if (await $.fs.exists(config.disabled_marker)) {
        $.ui.log('the warden gave up on this session; stopping this listener', { to: 'debug' })
        return
      }
      await $.fs.write(config.loaded_marker, 'loaded\n')
      ready = true
    }
    const { code, signal } = await child.result
    $.ui.log(`ocagent listen exited (code ${code}, signal ${signal})`, { to: 'debug' })
  } catch (err) {
    $.ui.log(`ocagent listen stopped: ${String(err)}`, { to: 'debug' })
  }
}
