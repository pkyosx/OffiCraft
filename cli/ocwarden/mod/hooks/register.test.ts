import type { On } from 'claude-code'
import { expect, test } from 'claude-code/testing'

// What the warden writes beside the mod for member m1 (notifymod.go).
const CONFIG_M1 =
  '{"boot_prompt":"開始。","loaded_marker":"/w/m1/.officraft-mod-loaded",' +
  '"disabled_marker":"/w/m1/.officraft-mod-disabled","ack_file":"/w/m1/.officraft-listen-ack",' +
  '"ready_prefixes":["[ocagent] listen: connected","[ocagent] listen: disconnected"],' +
  '"listener":{"argv":["/w/m1/ocagent","listen","--deliver-mod"],"cwd":"/w/m1",' +
  '"env":{"OC_LISTEN_ACK":"1","OC_LISTEN_ACK_FILE":"/w/m1/.officraft-listen-ack"}}}\n'

const CONNECTED = '[ocagent] listen: connected — streaming http://127.0.0.1:7755/api/events [ts=1.000 local]\n'

type World = {
  config?: string | null
  stdout?: string[]
  stderr?: string[]
  refuse?: string[]
  throwOn?: string[]
  disabled?: boolean
  // The warden writes the disabled marker once the listener has started.
  disabledAfterSpawn?: boolean
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

  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('fs.read', ($, e) => {
    reads.push(e.path)
    const config = w.config === undefined ? CONFIG_M1 : w.config
    if (config === null || !e.path.endsWith('/officraft.json')) return { deny: `ENOENT: ${e.path}` }
    return { value: config }
  })
  on('fs.exists', ($, e) => ({ value: disabled && e.path === '/w/m1/.officraft-mod-disabled' }))
  on('fs.write', ($, e) => {
    writes.push({ path: e.path, text: e.text })
    trail.push(`write ${e.path}`)
    return { value: undefined }
  })
  on('prompt.submit', ($, e) => {
    submits.push({ text: e.text, asUser: e.origin.kind === 'plugin' && e.origin.asUser === true })
    trail.push(`submit ${e.text}`)
    if ((w.throwOn ?? []).includes(e.text)) throw new Error('the session is gone')
    return (w.refuse ?? []).includes(e.text) ? { drop: 'refused' } : { text: e.text }
  })
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
    if (/^(ocagent listen exited|the boot prompt was refused|the warden fell back)/.test(e.text)) finished()
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

const LISTENER = {
  argv: ['/w/m1/ocagent', 'listen', '--deliver-mod'],
  cwd: '/w/m1',
  env: { OC_LISTEN_ACK: '1', OC_LISTEN_ACK_FILE: '/w/m1/.officraft-listen-ack' },
  input: undefined,
}

test('under a session start, the boot prompt goes in first, then the listener, then the marker and each payload', async ($, on) => {
  const w = world(on, {
    stderr: [CONNECTED],
    stdout: ['{"submit":"[ocagent] chat from owner (#c-1): 甲\\n    第二行', '"}\n{"batch":"1"}\n'],
  })

  expect(await $.session.start(START)).toEqual({ cwd: '/w/m1' })
  await settled(w.done)

  // The plugin's own folder: in a member, <workdir>/.officraft-mod.
  expect(w.reads.length).toBe(1)
  expect(w.reads[0]?.endsWith('/mod/officraft.json')).toBe(true)
  expect(w.spawned).toEqual([LISTENER])
  expect(w.trail).toEqual([
    'submit 開始。',
    'spawn',
    'write /w/m1/.officraft-mod-loaded',
    'submit [ocagent] chat from owner (#c-1): 甲\n    第二行',
    'write /w/m1/.officraft-listen-ack',
  ])
  expect(w.submits).toEqual([
    { text: '開始。', asUser: true },
    { text: '[ocagent] chat from owner (#c-1): 甲\n    第二行', asUser: false },
  ])
  expect(w.writes).toEqual([
    { path: '/w/m1/.officraft-mod-loaded', text: 'loaded\n' },
    { path: '/w/m1/.officraft-listen-ack', text: 'ack 1\n' },
  ])
  expect(w.logs).toEqual([
    { text: CONNECTED, to: 'debug' },
    { text: 'ocagent listen exited (code 0, signal null)', to: 'debug' },
  ])
})

test('under a listener whose first output is a frame, that frame marks the mod loaded', async ($, on) => {
  const w = world(on, { stdout: ['{"submit":"[ocagent] listen: disconnected — dial tcp: refused"}\n'] })

  await $.session.start(START)
  await settled(w.done)

  expect(w.trail).toEqual([
    'submit 開始。',
    'spawn',
    'write /w/m1/.officraft-mod-loaded',
    'submit [ocagent] listen: disconnected — dial tcp: refused',
  ])
})

test('under a listener that refuses to start, no marker is written', async ($, on) => {
  // Positive control: the first test, where a transport line writes it.
  const w = world(on, {
    stderr: ['[ocagent] listen: --deliver-mod needs OC_SESSION (…); refusing to start.\n'],
  })

  await $.session.start(START)
  await settled(w.done)

  expect(w.spawned).toEqual([LISTENER])
  expect(w.writes).toEqual([])
})

for (const [name, refusal] of [
  ['refuses', { refuse: ['開始。'] }],
  ['throws on', { throwOn: ['開始。'] }],
] as const) {
  test(`under a boot prompt the session ${name}, nothing is marked and no listener starts`, async ($, on) => {
    const w = world(on, { ...refusal, stderr: [CONNECTED] })

    await $.session.start(START)
    await settled(w.done)
    // The refusal is logged first; a listener started after it would land here.
    await new Promise(resolve => setTimeout(resolve, 50))

    expect(w.spawned).toEqual([])
    expect(w.writes).toEqual([])
    expect(w.logs).toEqual([{ text: 'the boot prompt was refused; not listening', to: 'debug' }])
  })
}

test('under the warden falling back before the listener connected, the mod marks nothing and stops the listener', async ($, on) => {
  const w = world(on, { disabledAfterSpawn: true, stderr: [CONNECTED], stdout: ['{"submit":"甲"}\n'] })

  await $.session.start(START)
  await settled(w.done)

  expect(w.writes).toEqual([])
  expect(w.submits).toEqual([{ text: '開始。', asUser: true }])
})

for (const [name, failure] of [
  ['refused', { refuse: ['乙'] }],
  ['that throws', { throwOn: ['乙'] }],
] as const) {
  test(`under a submit ${name}, its batch is nacked and the next batch starts clean`, async ($, on) => {
    const w = world(on, {
      ...failure,
      stderr: [CONNECTED],
      stdout: [
        '{"submit":"甲"}\n{"submit":"乙"}\n{"submit":"丙"}\n{"batch":"1"}\n',
        '{"submit":"丁"}\n{"batch":"2"}\n',
      ],
    })

    await $.session.start(START)
    await settled(w.done)

    expect(w.submits.map(s => s.text)).toEqual(['開始。', '甲', '乙', '丙', '丁'])
    expect(w.writes).toEqual([
      { path: '/w/m1/.officraft-mod-loaded', text: 'loaded\n' },
      { path: '/w/m1/.officraft-listen-ack', text: 'nack 1\n' },
      { path: '/w/m1/.officraft-listen-ack', text: 'ack 2\n' },
    ])
  })
}

test('under stdout that is no frame, the line goes to the debug log and nothing is submitted', async ($, on) => {
  const w = world(on, {
    stderr: [CONNECTED],
    stdout: ['flag provided but not defined: -x\n{"other":1}\n'],
  })

  await $.session.start(START)
  await settled(w.done)

  expect(w.submits).toEqual([{ text: '開始。', asUser: true }])
  expect(w.logs).toEqual([
    { text: CONNECTED, to: 'debug' },
    { text: 'flag provided but not defined: -x', to: 'debug' },
    { text: '{"other":1}', to: 'debug' },
    { text: 'ocagent listen exited (code 0, signal null)', to: 'debug' },
  ])
})

test('under the warden having fallen back to pasting, the mod submits nothing and starts no listener', async ($, on) => {
  // Positive control: the first test, where the same start boots and listens.
  const w = world(on, { disabled: true, stderr: [CONNECTED] })

  expect(await $.session.start(START)).toEqual({ cwd: '/w/m1' })

  expect(w.trail).toEqual([])
})

// Positive control for these three: the first test, where a readable config
// starts the listener.
for (const [name, config] of [
  ['a missing config', null],
  ['a config without the listener', '{"boot_prompt":"開始。","loaded_marker":"/w/m1/.officraft-mod-loaded"}\n'],
  ['a config that is not JSON', 'not json'],
] as const) {
  test(`under ${name}, the mod submits nothing, marks nothing and starts no listener`, async ($, on) => {
    const w = world(on, { config, stderr: [CONNECTED] })

    expect(await $.session.start(START)).toEqual({ cwd: '/w/m1' })

    expect(w.trail).toEqual([])
    expect(w.logs.length).toBe(1)
    expect(w.logs[0]?.to).toBe('debug')
  })
}
