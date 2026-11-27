# Basic definition of a task manual

A task manual defines **when a kind of work should be created as a task, what information execution needs, and how it should be done from start to delivery**.

## Display name

A short name that lets whoever creates a task quickly recognize what kind of work this is.

## What is this task?

Written for whoever creates tasks, to judge **what work should be created as this kind of task**.

Explain what work this kind of task handles. If it is easily confused with other task manuals, the scope of application of each should be evident from the content.

Keep only the information needed to judge whether this kind of task should be created; do not include execution steps, handling details, or how to handle individual situations.

Do not write information that changes with the current situation as fixed rules. A task manual should describe the long-term scope, process, and way of judging for this kind of work, and does not describe the division of labor or scope of application by the names of specific members, roles, or other task manuals. When you need to judge who should handle it or which task manual to use, judge it at execution time based on the settings and definitions at that time.

## What information is needed?

Define **what information identifies the target to be handled this time** when this kind of task is created.

Find the information that best represents "whether this is the same piece of work" and use it as the identifying field, to judge whether an identical unfinished task already exists.

Besides the identifying field, add a separate field only for information that must be available before execution and is different every time; put background and supplementary explanations in the task description, not in separate fields.

## How is it done?

Written for the task's executor, to define **how this kind of task should be executed from start to completion**.

Split it into steps following the actual workflow, and state clearly what each step does and what condition must be reached before it counts as complete or the next step can begin.

When needed, it can include the execution flow, judgment conditions, approval gates, exception handling, and other rules that affect whether the work is executed correctly.

Use the following structure:

```markdown
# <Background, principles, or terms to understand first> (write only when needed)

# SOP

## 1. <Step name>

<What this step does, and the rules to follow.>

**Completion condition**: <an observable or verifiable result>

## 2. <Step name>

...

# Appendix A: <Name> (write only when needed)

<Shared rules or reference information that the main text cites during execution but that does not fit directly into a single step.>
```

Every step should have a clear completion condition, so the executor knows when to move on to the next step.

A completion condition should be an observable or verifiable result; do not use descriptions that cannot be judged directly, such as "handling complete" or "confirmed no problems".

When approval from the Owner or someone else is needed to continue, the corresponding step should state explicitly who approves and what the passing condition is.

Write only content that is **common to this kind of task and affects whether the work can be executed correctly**. Necessary processes, judgment conditions, exceptions, and caveats can be written directly into the task manual.

Do not enumerate large numbers of hypothetical scenarios to cover every possibility in advance; add a corresponding rule only for situations confirmed to affect how execution is done.

Content that belongs to this kind of work and applies no matter who executes it goes in the task manual; ways of collaborating, judgment principles, and trade-off criteria that apply only to a specific role go in that role's Insight.

An appendix is not a required part of the structure. When a piece of content is cited by multiple steps, or is fairly complete and would disrupt the main flow if placed directly in the SOP, it can be organized as an appendix and explicitly cited in the steps that use it.

An appendix is still part of the task manual; put in it only content common to this kind of task that affects whether the work is executed correctly, and do not organize knowledge unrelated to the task into it.

## Assigned executor

Specify who executes this kind of task by default, and the runtime used. When it is given to outsourcing, specify the execution machine.

# SOP

## 1. Clarify intent

Before starting the design, first confirm what work the Owner wants this kind of task to handle, and what problem the Owner wants solved.

First organize your current understanding based on the task description and the information the Owner has already provided, then open a reply card to confirm with the Owner. Do not break what the Owner has already said into questions and ask them again; when the information is already sufficient, simply summarize your understanding and ask the Owner to confirm it.

If the current information is insufficient, ask for the missing information on the same card. At minimum, you must be able to confirm:

- **What to do**: the scope of work this kind of task is responsible for.
- **What counts as done**: what state this kind of work must reach to count as complete.

This step clarifies only the work itself; do not speculate on your own about the task process, the information needed, or the assigned executor.

**Completion condition**: the Owner has confirmed what this kind of task does and what counts as done.

## 2. Handle "What is this task?"

Based on the scope of work the Owner confirmed, create or adjust the task definition according to the content rules of "What is this task?".

Before designing, look at the existing task manuals to check whether there is already an identical or similar task type. If scopes overlap or are adjacent, clarify the scope of application of each, so whoever creates a task can judge which manual to use.

When adjusting an existing task manual, build on the current content, keep the parts that are still correct and unaffected by this request, and change only what needs to change this time.

If information is insufficient and would affect the task's scope or its boundary with other manuals, open a reply card to ask the Owner.

**Completion condition**: the draft of "What is this task?" is complete, with no unresolved questions about the task's scope or manual boundaries.

## 3. Handle "How is it done?"

Based on the scope of work and completion conditions the Owner confirmed, create or adjust the execution flow according to the content rules of "How is it done?".

When creating a new task manual, first understand how this kind of work is actually executed today and the related rules, then organize the SOP following the actual order of the work.

When adjusting an existing task manual, first look at the current SOP, related rules, and task Lore to confirm which practices are still correct and which need adjusting. Keep content unaffected by this request, and change only the parts that need to change this time.

Each step should state what to do, the rules to follow, and what condition must be reached before moving on to the next step.

If missing information would affect the execution flow, completion conditions, approval gates, or other important handling, open a reply card to ask the Owner.

**Completion condition**: an SOP that lets the executor move from start to completion of the work is done, and every step's completion condition and the necessary approval gates are clear.

## 4. Handle "What information is needed?"

First identify **which target is handled** each time this kind of task is executed, then find the information best suited to point to that target and use it as the task's identifying field.

For example, when handling a Jira issue, you can use the Jira ticket; when reviewing a PR, you can use the PR.

Then check whether there is any other information that must be available before execution and is different every time, which needs a separate field.

If you cannot judge the work target or a suitable identifying field, open a reply card to ask the Owner.

**Completion condition**: the identifying field for this kind of task, and any other necessary fields, have been confirmed.

## 5. Handle "Assigned executor"

Based on this kind of task's work and SOP, work out a suitable assigned executor and runtime, and open a reply card asking the Owner to decide.

By default, recommend that **outsourcing** executes it, and additionally offer suitable **staff member candidates** for the Owner to choose from.

For the runtime, by default recommend **Claude, Opus, Medium effort**, and offer other suitable configuration options for the Owner to choose from according to the work's needs.

When outsourcing is chosen as the assigned executor, also propose the execution machine; if no machine is specified, or the specified machine is offline, the task will not start executing.

**Completion condition**: the Owner has confirmed this kind of task's assigned executor and runtime; when given to outsourcing, this includes the execution machine.

## 6. Review the complete task manual

Start a new sub-agent as the Reviewer, and give it this manual's rules for each part of a task manual, along with the complete task manual produced this time. The Reviewer did not take part in the design so far and checks from an independent angle whether the final content meets this manual's definitions.

The Reviewer checks:

- "What is this task?" is enough for whoever creates tasks to judge what work should use this manual, and its scope of application relative to similar task manuals is clear.
- "What information is needed?" clearly identifies the target this kind of task handles each time, and every other field is information that must be available before execution.
- "How is it done?" is enough for the executor to move all the way from start to completion, with no missing necessary steps, rules, or approval gates.
- Every step's completion condition is observable or verifiable.
- The task manual does not mix in ways of collaborating, judgment principles, or trade-off criteria that apply only to a specific role.
- Content in appendices is actually used in executing the task, and is explicitly cited where the main text needs it.
- Overall there is no duplicated, contradictory, or unnecessary content.

When the Reviewer finds problems, revise according to the review comments, then hand it back to the Reviewer to check. If a problem involves the task's scope, completion conditions, way of execution, or other matters needing an Owner decision, open a reply card to ask the Owner, then revise.

**Completion condition**: the Reviewer confirms that the final task manual meets this manual's content definitions and is sufficient for the executor to complete this kind of task.

## 7. Owner approval

After the review passes, open a reply card asking the Owner to approve the final version.

When creating a new task manual, provide the complete task manual content and the assigned executor.

When adjusting an existing task manual, for **"What is this task?" and "How is it done?"** use `ocagent diff <before> <after>` to generate a before/after diff and attach the diff link to the card; list other adjustments directly on the card, and provide the complete revised version for the Owner to approve.

When the Owner asks for changes, adjust the content according to the reply; if the changes affect the task manual's substantive content, run the review again, and after it passes, ask the Owner to approve again.

**Completion condition**: the Owner has approved the final task manual and assigned executor.

## 8. Write and read back

Create or update the task manual according to the Owner-approved version. If you cannot write it yourself, open a reply card asking the Owner to perform the necessary operation.

After writing, read the task manual back and confirm that the display name, "What is this task?", "What information is needed?", "How is it done?", and the assigned executor all match the Owner-approved version.

**Completion condition**: the task manual has been updated, and what reads back matches the Owner-approved version.