// Focused browser checks for the user-operation contract list.
//
// 13_reply_cards.spec.js owns the larger historical loop and remains behaviorally
// unchanged; this file adds the single-card draft-preservation checks on every
// ReplyCardBody caller so the two sentences in docs/guide/quickstart.md:88 do not
// share one vague assertion.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

function options(texts, aiPickAt) {
  return texts.map((text, i) => ({ text, ai_pick: i === aiPickAt }));
}

function repliesTab(page) {
  return page.locator('.nav-tab', { hasText: '請示' });
}

function chip(scope, idx) {
  return scope.locator(
    `[data-testid="reply-option"][data-option-idx="${idx}"]`,
  );
}

// 🔴 T-91: THE CREATE ANSWERS A RECEIPT, NOT THE CARD. POST /api/reply-cards
// used to hand back the whole ReplyCardDTO; it now answers
// {id, chat_message_id, created_ts, attachments} — every field the handler
// MINTED, and nothing the caller sent. So this helper creates, then READS THE
// CARD BACK, which is the same "write then re-read" the cockpit itself moved to
// in this package. Returning the receipt directly would make every downstream
// `.status` / `.options` / `.select_mode` assertion silently `undefined`.
async function createReplyCardAs(request, agentToken, card) {
  const res = await request.post(`${BASE}/api/reply-cards`, {
    headers: authHeaders(agentToken),
    data: { linked_task: null, ...card },
  });
  expect(res.status(), 'creating a reply card must succeed').toBe(200);
  const receipt = await res.json();
  expect(receipt.id, 'the create receipt must name the card it minted').toBeTruthy();
  return readReplyCardAs(request, agentToken, receipt.id);
}

async function readReplyCardAs(request, token, cardId) {
  const res = await request.get(`${BASE}/api/reply-cards/${cardId}`, {
    headers: authHeaders(token),
  });
  expect(res.status(), 'reading the answered reply card must succeed').toBe(200);
  return res.json();
}

// 🔴 T-91: THE CREATE ANSWERS A RECEIPT, NOT THE TASK. POST /api/tasks used to
// answer {task: {...}, deduped}; it now answers
// {task_id, task_no, title, status, deduped} with no `task` key at all. This
// helper therefore creates, takes the minted id off the receipt, and READS THE
// TASK BACK, so callers keep getting a real TaskDTO.
async function createTaskAs(request, token, title, executorId) {
  const res = await request.post(`${BASE}/api/tasks`, {
    headers: authHeaders(token),
    data: { title, executor_member_id: executorId },
  });
  expect(res.status(), 'creating a task must succeed').toBe(200);
  const receipt = await res.json();
  expect(
    receipt.task_id,
    'task create must name the ticket it minted (task_id, top level)',
  ).toBeTruthy();
  return readTaskAs(request, token, receipt.task_id);
}

async function submitPlanAs(request, token, taskId) {
  const res = await request.post(`${BASE}/api/tasks/${taskId}/plan`, {
    headers: authHeaders(token),
    data: {
      steps: [{ name: 'UOC reply step', dod: 'The owner can answer this step.' }],
    },
  });
  expect(res.status(), 'submitting a task plan must succeed').toBe(200);
}

async function readTaskAs(request, token, taskId) {
  const res = await request.get(`${BASE}/api/tasks/${taskId}`, {
    headers: authHeaders(token),
  });
  expect(res.status(), 'reading task detail must succeed').toBe(200);
  return res.json();
}

async function setStepInProgressAs(request, token, taskId, stepId) {
  const res = await request.post(
    `${BASE}/api/tasks/${taskId}/steps/${stepId}/status`,
    {
      headers: authHeaders(token),
      data: { status: 'in_progress' },
    },
  );
  expect(res.status(), 'starting the task step must succeed').toBe(200);
}

test.describe('user-operation contract · single card draft preservation', () => {
  test('a page one-tap answer carries the text already in its composer', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, uniqueName('UOC page member'));
    const memberToken = await mintMemberToken(request, token, member.id, 1);
    const summary = uniqueName('單選頁面草稿');
    const draft = uniqueName('這段字不能丟');
    const card = await createReplyCardAs(request, memberToken, {
      kind: 'decision',
      summary,
      options: options(['保留', '送出'], 1),
      select_mode: 'single',
    });

    await bootAuthedSpa(page, token);
    await repliesTab(page).click();
    const waiting = page.getByTestId('waiting-card').filter({ hasText: summary });
    await expect(waiting).toBeVisible();
    await waiting.locator('.chat__input').fill(draft);
    await chip(waiting, 1).click();

    await expect(waiting, 'one tap must close the page card').toHaveCount(0);
    await page.getByTestId('answered-toggle').click();
    const answered = page
      .getByTestId('answered-card')
      .filter({ hasText: summary });
    await expect(answered).toBeVisible();
    const finalAnswer = answered.getByTestId('final-answer');
    // UOC_ASSERT id=UOC-RC-SINGLE-DRAFT screen=replies-page name=single_option_keeps_draft_on_replies_page
    await expect(
      finalAnswer,
      'the page one-tap answer must keep the draft text',
    ).toContainText(draft);

    const readback = await readReplyCardAs(request, memberToken, card.id);
    expect(readback.answer.option_idxs).toEqual([1]);
    expect(readback.answer.text).toBe(draft);
  });

  test('a chat one-tap answer carries the text already in its composer', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, uniqueName('UOC chat member'));
    const memberToken = await mintMemberToken(request, token, member.id, 1);
    const summary = uniqueName('單選聊天草稿');
    const draft = uniqueName('聊天字不能丟');
    const card = await createReplyCardAs(request, memberToken, {
      kind: 'decision',
      summary,
      options: options(['保留', '送出'], 1),
      select_mode: 'single',
    });

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: member.name }).click();
    const chatCard = page.locator(
      `[data-testid="chat-reply-card"][data-reply-card-id="${card.id}"]`,
    );
    await expect(chatCard).toBeVisible();
    // A chat card mounts COLLAPSED since owner c-6f054c1cb481 (2026-09-04) —
    // the composer this contract is about only exists once it is opened. The
    // 請示 page and the task-page embed above/below did NOT change.
    await chatCard.getByTestId('chat-reply-card-expand').click();
    await chatCard.locator('.chat__input').fill(draft);
    await chip(chatCard, 1).click();

    // UOC_ASSERT id=UOC-RC-SINGLE-DRAFT screen=chat-page name=single_option_keeps_draft_in_chat
    await expect(
      chatCard.getByTestId('final-answer'),
      'the chat one-tap answer must keep the draft text',
    ).toContainText(draft);

    const readback = await readReplyCardAs(request, memberToken, card.id);
    expect(readback.answer.option_idxs).toEqual([1]);
    expect(readback.answer.text).toBe(draft);
  });

  test('a task-page embedded one-tap answer carries the text already in its composer', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const member = await hireMember(request, token, uniqueName('UOC task member'));
    const memberToken = await mintMemberToken(request, token, member.id, 1);
    const title = uniqueName('UOC 任務內嵌卡');
    const summary = uniqueName('任務內嵌單選');
    const draft = uniqueName('任務字不能丟');
    const task = await createTaskAs(request, token, title, member.id);
    await submitPlanAs(request, memberToken, task.id);
    const planned = await readTaskAs(request, memberToken, task.id);
    const step = planned.steps[0];
    expect(step, 'the planned task must return its reply-card step').toBeTruthy();
    await setStepInProgressAs(request, memberToken, task.id, step.id);
    const card = await createReplyCardAs(request, memberToken, {
      kind: 'decision',
      summary,
      options: options(['保留', '送出'], 1),
      select_mode: 'single',
      linked_task: { task_id: task.id, step_id: step.id },
    });

    await bootAuthedSpa(page, token);
    await page.locator('.nav-tab', { hasText: '任務' }).click();
    const taskCard = page.getByTestId('task-card').filter({ hasText: title });
    await expect(taskCard).toBeVisible();
    await taskCard.locator('.task-card__head').click();
    const embedded = taskCard.locator(
      `[data-testid="task-reply-card"][data-reply-card-id="${card.id}"]`,
    );
    await expect(embedded).toBeVisible();
    await embedded.locator('.chat__input').fill(draft);
    await chip(embedded, 1).click();

    // UOC_ASSERT id=UOC-RC-SINGLE-TAP screen=tasks-page name=single_option_tap_answers_on_tasks_page
    await expect(
      embedded.getByTestId('final-answer'),
      'one tap on the task-page option must answer the embedded card',
    ).toBeVisible();
    // UOC_ASSERT id=UOC-RC-SINGLE-DRAFT screen=tasks-page name=single_option_keeps_draft_on_tasks_page
    await expect(
      embedded.getByTestId('final-answer'),
      'the task-page one-tap answer must keep the draft text',
    ).toContainText(draft);

    const readback = await readReplyCardAs(request, memberToken, card.id);
    expect(readback.answer.option_idxs).toEqual([1]);
    expect(readback.answer.text).toBe(draft);
  });
});
