# Manage AI accounts and usage

After reading this page, you can use the Monitor tab to see how much each account has used and roughly what it has cost, and which account and model each member is on. You can also sign machines in to an account and upgrade Claude Code from there.

## Whose account the usage counts against

Members use your own Claude and Codex accounts, so all usage and cost count against your accounts. Conversations are sent to Anthropic or OpenAI as usual.

## Viewing usage in the Monitor tab

Open the Monitor tab to see:

- **Accounts**: how much each account has used in its "5-hour window" and "7-day window". When an account is being used up too fast, it is marked "overheated"; when it runs out, it is marked "Limit reached" along with the reset time.
- **est.**: each account's accumulated estimated cost.
- **AI Sessions**: which machine each member is on, which account and model it uses, and roughly how much it has cost.

For a full description of every field, see "Monitor" in [Interface reference](interface.md).

## Where the usage figures come from

About every 5 minutes, and each time a Claude member starts, each machine's warden takes the Claude login that members on this machine use and asks Anthropic directly: which account this login belongs to, and how much of the 5-hour and 7-day windows it has used. "Accounts" shows exactly that answer, and "Measured X ago" next to the figure is when it was measured.

- When the lookup fails (for example the login has expired, or Anthropic does not respond), the account shows "Usage unknown" with the reason; old or estimated figures are never shown in its place. When the account cannot be identified, the reason appears in the Claude column under "Machines".
- When no machine is signed in to an account any more, its last measured figures and time are kept until that window resets; figures from other accounts never overwrite them.
- On macOS, one machine can hold two Claude logins at the same time: one in the Keychain and one in `~/.claude/.credentials.json` (for example, signing in over ssh can only write the file). Members use the Keychain one, and that is the one measured here; the file is used only when the Keychain has no such item or what it holds is not login data. When the Keychain item can be read but holds no usable login, Claude Code does not fall back to the file either, so this shows "Usage unknown" instead of measuring the file.
- The warden only reads the login. It never refreshes or rewrites it, and never sends its contents to the OffiCraft server.

## Signing in to an account and upgrading Claude Code

"Machines" in the Monitor tab lists every machine. Click the version number in the Claude or Codex column to open a menu:

- **Sign in**: sign this machine in to a Claude or Codex account. Members on this machine then use that account. If the machine is already signed in, it switches to the newly signed-in account once you finish.
- **Upgrade Claude Code**: upgrade the Claude Code that members on this machine use.

## Changing the Claude account while members are running

When a Claude member starts, the warden finds out which account its login belongs to in the same way, and from that moment the member counts against that account until it restarts. If the lookup fails (for example the members on this machine were idle so long that the login's access token expired, and it waits for the member's own Claude Code to refresh it), the member's account is empty. The first time after the member starts that the warden identifies this machine's login, if that account matches the last account found before the member started, the member is recorded under it; this happens at most once per start. If no account was ever found before the member started, or the machine was signed in to a different account in between, nothing is filled in and the member stays "account unknown" until it restarts, because there is no way to be sure which account it uses. Estimated cost from before the account is filled in is not counted against any account. Restarting the warden (including an automatic update) forgets members that have not been filled in yet; those members also stay "account unknown". After the machine is signed in to another account, members already running keep using the old account, and their usage and estimated cost keep counting against it. Only after a member is refocused, woken, or comes back online does it switch to the new account.

During that time, the account in "AI Sessions" shows "Still on its old account — switches after Refocus" below the old account. To switch a member right away, press "Refocus" on it. Cost already recorded against an account before the switch is not moved.

## When an account runs out

When an account's usage runs out, an exclamation mark appears next to the name of every member using that account, with the reason "usage limit reached". The members recover once the reset time arrives. For the other reasons behind the exclamation mark and what to do about them, see "Reading a member's status" in [Adding members and hiring outsource](members.md).

## Related pages

- [Assigning work and answering reply cards](daily.md)
- [Adding members and hiring outsource](members.md)
- [Making members remember what you taught them (Lore)](lore.md)
- [Interface reference](interface.md)
