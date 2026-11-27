# Basic definition of a role

A role consists of the following:

## Role name

A short name that lets other members quickly recognize what work this role is responsible for.

## Duty

Written for other members, to judge **what matters should go to this role**.

Use the following structure:

```text
# <Role name>

<One sentence on who this role is and the main scope it is responsible for.>

- **<Responsibility>**: <When to come to me, and what I will be responsible for.>
- **<Responsibility>**: <When to come to me, and what I will be responsible for.>
```

Write the systems, domains, or knowledge the role needs to master directly into the corresponding responsibility; for responsibilities easily confused with other roles, the division of labor should be evident from the content.

Keep only the information other members need to judge whether to come to this role; do not include execution steps, judgment details, or how to handle individual situations.

**The Duty should describe long-term responsibilities and division of labor, and does not record information that changes with the current situation.** It does not describe the division of labor or the boundaries of responsibility by the names of specific members, other roles, or task manuals. When you need to judge who should handle some other work, judge it at the time the work is actually assigned, based on the settings and definitions at that time.

## Insight

Written for the role itself, to supplement **the judgment principles it must follow over the long term when doing its work**.

Write it only when the role needs additional ways of collaborating, judgment principles, or trade-off principles; if there is nothing to add, do not write it — do not pad it out just to fill the format.

Use the following structure:

```text
# <Responsibility or Insight topic>

<Ways of working together, judgment principles, and the necessary reasons.>
```

Insight states "how to judge and why"; it does not state how tools are operated or how work is executed step by step — that kind of content belongs in task manuals or tool descriptions.

---

# SOP

## 1. Clarify intent

First, based on the task description and the information the Owner has already provided, confirm the purpose of creating or modifying the role, and the scope this role is expected to be responsible for.

When the information is already enough to start designing, there is no need to open another card to confirm; open a reply card to ask the Owner only when important information that affects the role's scope or design is missing, and do not ask again about content already provided.

**Completion condition**: you can clearly state the purpose of creating or modifying this role and the scope it is expected to be responsible for.

## 2. Handle the Duty

Check whether there are already roles with the same or similar responsibilities, understand the existing division of labor, then draft the Duty according to the direction the Owner confirmed.

The Duty should let other members judge what matters should go to this role from this content alone.

If its responsibilities are close to an existing role's, clarify the scope of application of each.

If adding or adjusting the role may affect the boundaries of responsibility of existing roles, open a reply card asking the Owner to confirm how to handle it:

- Do not adjust the existing roles.
- Adjust them in this task as well.
- Create a separate role-adjustment task.

If it is only a small boundary adjustment needed to keep the roles consistent with each other, recommend handling it in this task as well; if it would substantively change another role's positioning, main responsibilities, or Insight, recommend creating a separate role-adjustment task rather than expanding the scope of changes in this task.

When modifying an existing role, keep content that is unaffected by this request and still correct; do not rewrite for the sake of rewriting.

If there is still important information affecting the role's responsibilities or division of labor that you cannot judge, open a reply card to confirm with the Owner.

**Completion condition**: the Duty draft is complete, with clear responsibilities and boundaries; existing roles that may be affected have also been handled per the Owner's decision.

## 3. Handle the Insight

Based on the Duty, create or adjust the Insight according to the content rules of "Insight", and confirm whether there are ways of collaborating, judgment principles, or trade-off principles that this role must follow over the long term and that cannot be expressed through the Duty alone.

First look at the existing Insight and the Owner's past rulings about this role, keep content that is still correct and unaffected by this request, and only add or modify the parts needed this time. If the existing Insight already adequately supports the Duty, no change is needed.

If missing information would affect the role's ways of collaborating, judgment principles, or trade-offs, first look for the answer in existing material; if you still cannot judge, open a reply card asking the Owner to decide.

Additionally, open a reply card to confirm whether the Owner has specific knowledge sources this role should consult first when judging or handling problems; if so, organize "in what situations to consult which information" into the Insight.

If there is nothing to add, do not add Insight — do not pad it out for completeness.

**Completion condition**: the needed Insight is organized, and its content contains only role-level principles for collaboration, judgment, trade-offs, and knowledge references.

## 4. Review the complete role

Create an **independent sub-agent that did not take part in the design so far**, and give it this manual's rules for the Duty and Insight, along with the complete organized role content, for review.

At minimum, check:

- From the Duty alone, can other members judge what matters should go to this role?
- Are the boundaries of responsibility with other roles clear, without obvious overlap or contradiction?
- Does the Duty keep only the information needed for assigning work, without mixing in execution steps or judgment details?
- Does the Insight contain only role-level principles for collaboration, judgment, trade-offs, and knowledge references, without mixing in workflows or tool operation?
- Does it cite the names of specific members, other roles, or task manuals to describe the division of labor and boundaries of responsibility?
- Is there any duplicated, contradictory, or unnecessary content?

When the Reviewer finds problems, revise and review again; if a problem needs an Owner decision, open a reply card to confirm with the Owner, then adjust.

**Completion condition**: the Reviewer confirms that the complete role content meets the rules for the Duty and Insight, and is sufficient for actual use.

## 5. Owner approval

After the review passes, open a reply card asking the Owner to approve the final content.

When creating a new role, provide the complete role name, Duty, and Insight.

When modifying an existing role, use `ocagent diff <before> <after>` to generate a diff link so the Owner can directly check what this change modifies, and provide the complete revised version for reference.

If changes the Owner asks for would substantively change the role's responsibilities, division of labor, or Insight, review again after revising; adjustments that do not affect the meaning of the content, such as wording or formatting, do not need another review.

**Completion condition**: the Owner has approved the final role content.

## 6. Write and read back

Write the Owner-approved version to the site.

If the write cannot be completed, do not change the approved content on your own or take another approach; open a reply card to confirm with the Owner.

After writing, read the role content back and confirm the role name, Duty, and Insight all match the Owner-approved version.

**Completion condition**: the role has been written successfully, and the content read back matches the Owner-approved version.

## 7. Activate the member

After creating a new role and confirming it was written correctly, open one reply card asking the Owner to decide whether to wake the corresponding member right away, and on the same card provide configuration options for machine, model, and effort.

If the Owner chooses to wake it, start it with the configuration they chose; if they choose not to wake it for now, do not start the member.

When modifying an existing role, do not perform this step unless the change itself involves the member's activation state.

**Completion condition**: the Owner has decided whether to wake the member; when waking is needed, the member has been started with the configuration the Owner chose.

# Completion standard

The role content has been written according to the Owner-approved version and reads back identically; when creating a new role, whether to wake the corresponding member has also been confirmed and handled.