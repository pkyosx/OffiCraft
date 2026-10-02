import type { On } from 'claude-code'
import { expect, test } from 'claude-code/testing'

// What the warden writes beside the mod for member m1 (notifymod.go).
const CONFIG_M1 =
  '{"loaded_marker":"/w/m1/.officraft-mod-loaded","disabled_marker":"/w/m1/.officraft-mod-disabled",' +
  '"ack_file":"/w/m1/.officraft-listen-ack","listener":{"argv":["/w/m1/ocagent","listen","--deliver-mod"],' +
  '"cwd":"/w/m1","env":{"OC_LISTEN_ACK":"1","OC_LISTEN_ACK_FILE":"/w/m1/.officraft-listen-ack"}}}\n'

type World = {
  config?: string | null
  stdout?: string[]
  stderr?: string[]
  refuse?: string[]
  disabled?: boolean
}

// Everything beneath the mod: the files it touches, the prompts it submits,
// the child it spawns (its output scripted) and the lines it logs.
function world(on: On, w: World) {
  const writes: { path: string; text: string }[] = []
  const exists: string[] = []
  const reads: string[] = []
  const submits: string[] = []
  const spawned: unknown[] = []
  const logs: { text: string; to: string }[] = []
  let exited = () => {}
  const childDone = new Promise<void>(resolve => (exited = resolve))

  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('fs.read', ($, e) => {
    reads.push(e.path)
    const config = w.config === undefined ? CONFIG_M1 : w.config
    if (config === null || !e.path.endsWith('/officraft.json')) return { deny: `ENOENT: ${e.path}` }
    return { value: config }
  })
  on('fs.exists', ($, e) => {
    exists.push(e.path)
    return { value: w.disabled === true && e.path === '/w/m1/.officraft-mod-disabled' }
  })
  on('fs.write', ($, e) => {
    writes.push({ path: e.path, text: e.text })
    return { value: undefined }
  })
  on('prompt.submit', ($, e) => {
    submits.push(e.text)
    return (w.refuse ?? []).includes(e.text) ? { drop: 'refused' } : { text: e.text }
  })
  on('process.spawn', async function* ($, e) {
    spawned.push({ argv: e.argv, cwd: e.cwd, env: e.env, input: e.input })
    for (const text of w.stderr ?? []) yield { stream: 'stderr' as const, text }
    for (const text of w.stdout ?? []) yield { stream: 'stdout' as const, text }
    return { value: { code: 0, signal: null } }
  })
  on('ui.log', ($, e) => {
    logs.push({ text: e.text, to: e.to })
    if (e.text.startsWith('ocagent listen exited')) exited()
    return { value: undefined }
  })
  return { writes, exists, reads, submits, spawned, logs, childDone }
}

async function settled(done: Promise<void>) {
  let timer: ReturnType<typeof setTimeout> | undefined
  await Promise.race([
    done,
    new Promise((_, reject) => (timer = setTimeout(() => reject(new Error('the listener loop never ended')), 2000))),
  ])
  clearTimeout(timer)
}

const LISTENER = {
  argv: ['/w/m1/ocagent', 'listen', '--deliver-mod'],
  cwd: '/w/m1',
  env: { OC_LISTEN_ACK: '1', OC_LISTEN_ACK_FILE: '/w/m1/.officraft-listen-ack' },
  input: undefined,
}

test('under a session start, the mod marks itself loaded, submits each payload and acks the batch', async ($, on) => {
  const w = world(on, {
    stdout: ['{"submit":"[ocagent] chat from owner (#c-1): 甲\\n    第二行', '"}\n{"batch":"1"}\n'],
  })

  expect(await $.session.start({ cwd: '/w/m1', surface: null, isInteractive: false })).toEqual({ cwd: '/w/m1' })
  await settled(w.childDone)

  // The plugin's own folder: in a member, <workdir>/.officraft-mod.
  expect(w.reads.length).toBe(1)
  expect(w.reads[0]?.endsWith('/mod/officraft.json')).toBe(true)
  expect(w.spawned).toEqual([LISTENER])
  expect(w.submits).toEqual(['[ocagent] chat from owner (#c-1): 甲\n    第二行'])
  expect(w.writes).toEqual([
    { path: '/w/m1/.officraft-mod-loaded', text: 'loaded\n' },
    { path: '/w/m1/.officraft-listen-ack', text: 'ack 1\n' },
  ])
  expect(w.logs).toEqual([{ text: 'ocagent listen exited (code 0, signal null)', to: 'debug' }])
})

test('under a refused submit, its batch is nacked and the next batch starts clean', async ($, on) => {
  const w = world(on, {
    refuse: ['乙'],
    stdout: [
      '{"submit":"甲"}\n{"submit":"乙"}\n{"submit":"丙"}\n{"batch":"1"}\n',
      '{"submit":"丁"}\n{"batch":"2"}\n',
    ],
  })

  await $.session.start({ cwd: '/w/m1', surface: null, isInteractive: false })
  await settled(w.childDone)

  expect(w.submits).toEqual(['甲', '乙', '丙', '丁'])
  expect(w.writes).toEqual([
    { path: '/w/m1/.officraft-mod-loaded', text: 'loaded\n' },
    { path: '/w/m1/.officraft-listen-ack', text: 'nack 1\n' },
    { path: '/w/m1/.officraft-listen-ack', text: 'ack 2\n' },
  ])
})

test('under stderr and stdout that is no frame, the lines go to the debug log and nothing is submitted', async ($, on) => {
  const w = world(on, {
    stderr: ['[ocagent] listen: retrying in 4s\n'],
    stdout: ['flag provided but not defined: -x\n{"other":1}\n'],
  })

  await $.session.start({ cwd: '/w/m1', surface: null, isInteractive: false })
  await settled(w.childDone)

  expect(w.submits).toEqual([])
  expect(w.logs).toEqual([
    { text: '[ocagent] listen: retrying in 4s\n', to: 'debug' },
    { text: 'flag provided but not defined: -x', to: 'debug' },
    { text: '{"other":1}', to: 'debug' },
    { text: 'ocagent listen exited (code 0, signal null)', to: 'debug' },
  ])
})

test('under the warden having fallen back to pasting, the mod writes no marker and starts no listener', async ($, on) => {
  // Positive control: the first test, where the same start spawns the listener.
  const w = world(on, { disabled: true, stdout: ['{"submit":"甲"}\n'] })

  expect(await $.session.start({ cwd: '/w/m1', surface: null, isInteractive: false })).toEqual({ cwd: '/w/m1' })

  expect(w.exists).toEqual(['/w/m1/.officraft-mod-disabled'])
  expect(w.spawned).toEqual([])
  expect(w.writes).toEqual([])
  expect(w.submits).toEqual([])
})

// Positive control for these three: the first test, where a readable config
// starts the listener.
for (const [name, config] of [
  ['a missing config', null],
  ['a config without the listener', '{"loaded_marker":"/w/m1/.officraft-mod-loaded"}\n'],
  ['a config that is not JSON', 'not json'],
] as const) {
  test(`under ${name}, the mod marks nothing and starts no listener`, async ($, on) => {
    const w = world(on, { config, stdout: ['{"submit":"甲"}\n'] })

    expect(await $.session.start({ cwd: '/w/m1', surface: null, isInteractive: false })).toEqual({ cwd: '/w/m1' })

    expect(w.exists).toEqual([])
    expect(w.spawned).toEqual([])
    expect(w.writes).toEqual([])
    expect(w.logs.length).toBe(1)
    expect(w.logs[0]?.to).toBe('debug')
  })
}
