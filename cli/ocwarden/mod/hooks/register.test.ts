import type { On } from 'claude-code'
import { expect, test } from 'claude-code/testing'

// What the warden writes beside the mod for member m1 (notifymod.go).
const CONFIG_M1 =
  '{"boot_prompt":"開始。","started_marker":"/w/m1/.officraft-mod-started","loaded_marker":"/w/m1/.officraft-mod-loaded",' +
  '"disabled_marker":"/w/m1/.officraft-mod-disabled","booted_marker":"/w/m1/.officraft-mod-booted",' +
  '"ready_prefixes":["[ocagent] listen: connected","[ocagent] listen: disconnected"],' +
  '"listener":{"argv":["/w/m1/ocagent","listen","--deliver-socket"],"cwd":"/w/m1"}}\n'

// What clock.now answers, and the started marker the mod writes from it.
const NOW_MS = Date.UTC(2026, 9, 2, 3, 4, 5, 678)
const STARTED = { path: '/w/m1/.officraft-mod-started', text: '2026-10-02T03:04:05.678Z\n' }
const BOOTED = { path: '/w/m1/.officraft-mod-booted', text: 'booted\n' }
const LOADED = { path: '/w/m1/.officraft-mod-loaded', text: 'loaded\n' }

const CONNECTED = '[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [ts=1.000 local]\n'

type World = {
  config?: string | null
  stdout?: string[]
  stderr?: string[]
  refuse?: string[]
  throwOn?: string[]
  // Prompts whose submit hook throws before returning anything.
  throwSyncOn?: string[]
  disabled?: boolean
  // The warden writes the disabled marker once the listener has started.
  disabledAfterSpawn?: boolean
  // Writes to these paths fail.
  writeFails?: string[]
}

// Everything beneath the mod: the files it touches, the prompts it submits,
// the child it spawns (its output scripted) and the lines it logs, plus one
// ordered trail of the effects that must happen in sequence.
function world(on: On, w: World) {
  const trail: string[] = []
  const writes: { path: string; text: string }[] = []
  const reads: string[] = []
  const submits: { text: string; asUser: boolean }[] = []
  const spawned: unknown[] = []
  const logs: { text: string; to: string }[] = []
  let disabled = w.disabled === true
  let finished = () => {}
  const done = new Promise<void>(resolve => (finished = resolve))

  // The plugins and the engine beneath the mod's own hook.
  on('session.start', ($, e) => {
    trail.push('session.start beneath')
    return { cwd: e.cwd }
  })
  on('fs.read', ($, e) => {
    reads.push(e.path)
    const config = w.config === undefined ? CONFIG_M1 : w.config
    if (config === null || !e.path.endsWith('/officraft.json')) return { deny: `ENOENT: ${e.path}` }
    return { value: config }
  })
  on('fs.exists', ($, e) => ({ value: disabled && e.path === '/w/m1/.officraft-mod-disabled' }))
  on('fs.write', ($, e) => {
    if ((w.writeFails ?? []).includes(e.path)) return { deny: `EACCES: ${e.path}` }
    writes.push({ path: e.path, text: e.text })
    trail.push(`write ${e.path}`)
    return { value: undefined }
  })
  on('clock.now', () => ({ value: NOW_MS }))
  on('prompt.submit', ($, e) => {
    submits.push({ text: e.text, asUser: e.origin.kind === 'plugin' && e.origin.asUser === true })
    trail.push(`submit ${e.text}`)
    if ((w.throwSyncOn ?? []).includes(e.text)) throw new Error('the session is gone, synchronously')
    return answer(e.text)
  })
  const answer = async (text: string) => {
    if ((w.throwOn ?? []).includes(text)) throw new Error('the session is gone')
    return (w.refuse ?? []).includes(text) ? { drop: 'refused' } : { text }
  }
  on('process.spawn', async function* ($, e) {
    spawned.push({ argv: e.argv, cwd: e.cwd, env: e.env, input: e.input })
    trail.push('spawn')
    if (w.disabledAfterSpawn) disabled = true
    for (const text of w.stderr ?? []) yield { stream: 'stderr' as const, text }
    for (const text of w.stdout ?? []) yield { stream: 'stdout' as const, text }
    return { value: { code: 0, signal: null } }
  })
  on('ui.log', ($, e) => {
    logs.push({ text: e.text, to: e.to })
    if (/^(ocagent listen exited|the boot prompt was refused|the warden gave up)/.test(e.text)) finished()
    return { value: undefined }
  })
  return { trail, writes, reads, submits, spawned, logs, done }
}

async function settled(done: Promise<void>) {
  let timer: ReturnType<typeof setTimeout> | undefined
  await Promise.race([
    done,
    new Promise((_, reject) => (timer = setTimeout(() => reject(new Error('the mod never finished')), 2000))),
  ])
  clearTimeout(timer)
}

const START = { cwd: '/w/m1', surface: null, isInteractive: false } as const

// No env: the listener inherits this session's messaging socket variables.
const LISTENER = {
  argv: ['/w/m1/ocagent', 'listen', '--deliver-socket'],
  cwd: '/w/m1',
  env: undefined,
  input: undefined,
}

test('under a session start, the boot prompt goes in first, then the listener, then the load marker', async ($, on) => {
  const w = world(on, { stderr: [CONNECTED] })

  expect(await $.session.start(START)).toEqual({ cwd: '/w/m1' })
  await settled(w.done)

  // The plugin's own folder: in a member, <workdir>/.officraft-mod.
  expect(w.reads.length).toBe(1)
  expect(w.reads[0]?.endsWith('/mod/officraft.json')).toBe(true)
  expect(w.spawned).toEqual([LISTENER])
  expect(w.trail).toEqual([
    'write /w/m1/.officraft-mod-started',
    'session.start beneath',
    'submit 開始。',
    'write /w/m1/.officraft-mod-booted',
    'spawn',
    'write /w/m1/.officraft-mod-loaded',
  ])
  expect(w.submits).toEqual([{ text: '開始。', asUser: true }])
  expect(w.writes).toEqual([STARTED, BOOTED, LOADED])
  expect(w.logs).toEqual([
    { text: CONNECTED, to: 'debug' },
    { text: 'ocagent listen exited (code 0, signal null)', to: 'debug' },
  ])
})

test('under a listener whose first transport line is a disconnect, that line marks the mod loaded', async ($, on) => {
  const w = world(on, { stderr: ['[ocagent] listen: disconnected — dial tcp: refused\n'] })

  await $.session.start(START)
  await settled(w.done)

  expect(w.writes).toEqual([STARTED, BOOTED, LOADED])
})

for (const [name, output] of [
  ['a listener that refuses to start', { stderr: ['[ocagent] listen: --deliver-socket needs OC_SESSION (…); refusing to start.\n'] }],
  ['a listener that prints only on stdout', { stdout: ['[ocagent] listen: connected\n'] }],
] as const) {
  test(`under ${name}, no load marker is written`, async ($, on) => {
    // Positive control: the first test, where a transport line on stderr writes it.
    const w = world(on, output)

    await $.session.start(START)
    await settled(w.done)

    expect(w.spawned).toEqual([LISTENER])
    expect(w.writes).toEqual([STARTED, BOOTED])
  })
}

for (const [name, refusal] of [
  ['refuses', { refuse: ['開始。'] }],
  ['throws on', { throwOn: ['開始。'] }],
  ['throws synchronously on', { throwSyncOn: ['開始。'] }],
] as const) {
  test(`under a boot prompt the session ${name}, nothing is marked and no listener starts`, async ($, on) => {
    const w = world(on, { ...refusal, stderr: [CONNECTED] })

    await $.session.start(START)
    await settled(w.done)
    // The refusal is logged first; a listener started after it would land here.
    await new Promise(resolve => setTimeout(resolve, 50))

    expect(w.spawned).toEqual([])
    expect(w.writes).toEqual([STARTED])
    expect(w.logs).toEqual([{ text: 'the boot prompt was refused; not listening', to: 'debug' }])
  })
}

test('under the warden giving up before the listener connected, the mod writes no load marker and stops the listener', async ($, on) => {
  const w = world(on, { disabledAfterSpawn: true, stderr: [CONNECTED] })

  await $.session.start(START)
  await settled(w.done)

  expect(w.writes).toEqual([STARTED, BOOTED])
  expect(w.logs.at(-1)).toEqual({ text: 'the warden gave up on this session; stopping this listener', to: 'debug' })
})

test('under the warden having given up on the session, the mod marks its start, submits nothing and starts no listener', async ($, on) => {
  // Positive control: the first test, where the same start boots and listens.
  const w = world(on, { disabled: true, stderr: [CONNECTED] })

  expect(await $.session.start(START)).toEqual({ cwd: '/w/m1' })

  // The started marker still goes first: it is what dates a late session.start.
  expect(w.trail).toEqual(['write /w/m1/.officraft-mod-started', 'session.start beneath'])
  expect(w.writes).toEqual([STARTED])
  expect(w.logs).toEqual([
    { text: 'session.start found the warden already gave up on this session; not booting', to: 'debug' },
  ])
})

test('under a started marker that cannot be written, the failure is logged and the session boots as usual', async ($, on) => {
  // Positive control: the first test, where the marker is written.
  const w = world(on, { writeFails: ['/w/m1/.officraft-mod-started'], stderr: [CONNECTED] })

  await $.session.start(START)
  await settled(w.done)

  expect(w.trail).toEqual([
    'session.start beneath',
    'submit 開始。',
    'write /w/m1/.officraft-mod-booted',
    'spawn',
    'write /w/m1/.officraft-mod-loaded',
  ])
  expect(w.logs).toEqual([
    { text: 'cannot write /w/m1/.officraft-mod-started (HooksError: officraft: $.fs.write: EACCES: /w/m1/.officraft-mod-started)', to: 'debug' },
    { text: CONNECTED, to: 'debug' },
    { text: 'ocagent listen exited (code 0, signal null)', to: 'debug' },
  ])
})

// Positive control for these four: the first test, where a readable config
// starts the listener.
for (const [name, config] of [
  ['a missing config', null],
  ['a config without the listener', '{"boot_prompt":"開始。","loaded_marker":"/w/m1/.officraft-mod-loaded"}\n'],
  // Every field is required, the diagnostic started marker too: the mod writes nowhere it was not told.
  ['a config without the started marker', CONFIG_M1.replace('"started_marker":"/w/m1/.officraft-mod-started",', '')],
  ['a config that is not JSON', 'not json'],
] as const) {
  test(`under ${name}, the mod submits nothing, marks nothing and starts no listener`, async ($, on) => {
    const w = world(on, { config, stderr: [CONNECTED] })

    expect(await $.session.start(START)).toEqual({ cwd: '/w/m1' })

    expect(w.trail).toEqual(['session.start beneath'])
    expect(w.logs.length).toBe(1)
    expect(w.logs[0]?.to).toBe('debug')
  })
}
