# Making members remember what you taught them (Lore)

This page explains the documents members read when they start work: the "User additions" you write for all members, each role's documents under "Roles", and the "lore" members learn from their work, and what each of them means.

## User additions

"User additions" is a document you write for all members. Every member reads all of it each time it boots. It starts out blank, and it is the first document worth writing. It is under "User additions" in **Settings › Global context**.

Content that fits here:

- Who you are and what this studio does.
- Which data, accounts, or services members may touch, and which they must never touch.
- What members must always ask you about first by opening a reply card, for example sending email to people outside, changing a service that is in use, or spending money.
- The rough budget range, and how you prefer to receive reports.

User additions are sent to members in full every time they boot, so **keep them short**. For what the other documents in Global context are for, see "Global context" in [Settings](settings.md).

## Limits on risk and cost

When members make judgements, the top principle is: **you set the limits on risk**. Within the range you accept, members act on their own. When something could go over budget, could cause damage that cannot be undone (breaking a service that is in use, deleting data, leaking secrets), or means choosing a major direction that is hard to reverse, members stop and open a reply card to ask you.

Members work in the background with no one beside them to click a confirmation, so both Claude Code and Codex are set to skip permission prompts and act directly. **Members can do a lot on your computer. What restrains them is the rules they read and the practice of stopping to open a reply card when they should ask, not the system's permission controls.** Use OffiCraft only after you accept this. This is why the limits belong in User additions, and why steps that cost something should be set as gates in a task manual; see [Having AI follow an SOP for repeated work (Task manuals)](sop.md).

Members use your own Claude and Codex accounts, and the cost is charged to your accounts; see [Manage AI accounts and usage](usage.md).

## Roles

Each staff role has two documents, under **Settings › Roles**:

- **Duty**: what this role is responsible for, which rules it follows, and what is not this role's business. You and the Assistant, Mira, can edit it. It is the place to write "what should be brought to this role" and "what this role must not do". Give one-off things directly in chat, and write work that comes up again and again as a task manual.
- **Insight**: the trade-offs and limits this role uses again and again in its work, usually written by the role's own member.

After a change, the role's member reads the new version the next time it boots.

## Lore

Lore is a set of judgement principles that members have confirmed in their work and will use again; each entry records only one principle. A member writes an entry on its own when a task closes, when it is stopped, or when it judges that some experience is worth keeping; when there is nothing worth keeping, it writes nothing. The console has no button for you to add lore directly. To have members remember something, tell a member in chat and ask it to write it as lore.

Content members decide not to write as lore: one-off task information, details of a particular case or customer, and anything that can be looked up again in documents. When the experience shows that "the SOP itself should change", the member opens a reply card asking you whether to change the SOP, instead of writing lore in its place. When your judgement differs from what a member expected and the reason is unclear, the member first opens a reply card to ask you why, rather than guessing.

After lore is written, its title, body, and type can all be changed: on the "Lore" tab, click "Edit" on an entry to change its title and body, and click the type tag on an entry to change its type. Members can change the entries they wrote, and the Assistant can change any entry. When an entry is pinned, only you and the Assistant can change it. Changes do not alter an entry's ID, scope, state, or position in the list. To change the state or type, click the tag on the entry; to change the scope, click the scope tag on the entry (only you and the Assistant can click it). After you make a choice, the entry's border becomes dashed and the changed tag gets a ↺ in its top-right corner (hover over it to see the original value; clicking it reverts only that one item). You can go on to change several entries and then click "Save changes" in the toolbar at the bottom of the screen to save them all at once; after saving, the whole section is re-sorted by the new result. Changes to the title and body are saved as soon as you click "Save". See "Lore" in [Interface reference](interface.md).

While the author is still on the roster, the bottom of the entry card has a message box. A message sent there carries that entry's ID and goes to the author; this also applies to pinned entries. Entries you wrote, and entries whose author has left, have no message box on the card.

**Making a lore entry longer uses up more of the character cap, so entries further down the list may stop being read, and no error message is shown.** After saving, the characters used shown above that section, how many entries are read from each source, and the unread entries collapsed into single rows are all updated, so you can check there which entries are still read.

## Lore scope

Each lore entry has a scope, which decides who reads it:

- **Member lore**: only the member who wrote the entry reads it.
- **Task lore**: filed under a task manual; every member who later does that kind of task reads it.
- **Everyone**: every member reads it when it boots.

When a member writes a principle that applies to everyone, you can change that entry's scope to "Everyone" on the "Lore" tab, so other members do not each have to learn it again. Only you and the Assistant can change the scope.

The "Lore" tab is divided into sections by who reads the entries: one section for "Everyone", one for each member, and one for each task manual. When you open the tab, you first see a directory of these sections; click a section to see its entries. Each section lists exactly the lore that member reads when it boots (or that is read with that manual).

For what staff and outsource members each read when they boot, see "What members read when they boot" in [Architecture and how it works](architecture.md).

## Lore states and the character cap

Each lore entry has one of three states:

- **Active**: the normal state.
- **Pinned**: listed first and read first. When a member boots, pinned entries come before all other entries, whether they belong to "Everyone" or to the member itself. Only you and the Assistant can pin entries.
- **Retired**: members no longer read it. It can be set back to active.

**The lore members read has a character cap.** When the cap is exceeded, members do not read the older entries, and no error message is shown. When each member boots, the "Everyone" lore and the member's own lore are combined into one list: pinned entries first, then the rest from newest to oldest by effective date, with the two sources interleaved. Both share the same cap, counted from the top until the first entry that does not fit. The more or the longer the "Everyone" lore becomes, the fewer of each member's own older entries fit.

In the directory of the "Lore" tab, each section has a card showing the characters used, the cap (with a usage bar), and how many entries are not read. Inside a section, below the section title, a segmented usage bar shows how many characters the "Everyone" lore and the section's own lore each take up, followed by the characters used and the cap, and how many entries are read from each source (for example "Everyone 7/12 · Nova 0/3"). The "Everyone" section has no usage bar; below its title it shows only the characters used and "read by every member", because the "Everyone" lore counts toward each member's own cap. Entries members do not read are collapsed into a single row showing the entry ID, the title, and a red "Not read"; click the row to expand it into the full card. The same applies to entries found with filters or search. Retiring, pinning, shortening, or lengthening an entry, a member bumping an entry to the newest, and changing the character cap in **Settings › General** can all change which entries are not read.

For the full description of the Lore tab, see "Lore" in [Interface reference](interface.md).

## Version history

Each time User additions, a Duty, an Insight, or a task manual's SOP is saved, the older version is kept, so if a change breaks something you can compare the differences and restore it; see "Version history" in [Settings](settings.md).

When a lore entry's title or body is changed, the version before the change is also kept; the last 3 versions of each entry are kept. Changing only the type does not keep a version. The entry point is in the edit box of an entry on the "Lore" tab; see "Lore" in [Interface reference](interface.md).

## Related pages

- [Having AI follow an SOP for repeated work (Task manuals)](sop.md)
- [Adding members and hiring outsource](members.md)
- [Settings](settings.md)
- [Glossary](glossary.md)
